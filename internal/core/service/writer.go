package service

import (
	"context"
	"fmt"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/policy"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

// WriterService runs decoded telemetry through the policies (drop rules,
// attribute promotion, RED stats, tail sampling, dimension dedupe) and
// writes the result through the repository.
//
// It is not safe for concurrent use; the queue consumer drives it from one
// goroutine.
type WriterService struct {
	repo      port.TelemetryRepository
	filter    *policy.Filter
	sampler   policy.Sampler
	resources *policy.Seen
	series    *policy.Seen

	buf       domain.Batch
	spanStats []domain.SpanStat
}

var _ port.BatchWriter = (*WriterService)(nil)

type WriterDeps struct {
	Repo    port.TelemetryRepository
	Filter  *policy.Filter
	Sampler policy.Sampler
	// Resources and Series dedupe dimension rows across batches.
	Resources *policy.Seen
	Series    *policy.Seen
}

func NewWriterService(d WriterDeps) *WriterService {
	return &WriterService{
		repo:      d.Repo,
		filter:    d.Filter,
		sampler:   d.Sampler,
		resources: d.Resources,
		series:    d.Series,
	}
}

func (w *WriterService) Add(b domain.Batch, now time.Time) {
	for _, r := range b.Resources {
		if w.resources.ShouldWrite(r.ID, now) {
			w.buf.Resources = append(w.buf.Resources, r)
		}
	}

	for i := range b.Logs {
		if !w.filter.DropLog(&b.Logs[i]) {
			w.buf.Logs = append(w.buf.Logs, b.Logs[i])
		}
	}

	for i := range b.Spans {
		s := &b.Spans[i]
		if w.filter.DropSpan(s) {
			continue
		}
		policy.PromoteSpanAttributes(s)
		if s.IsEntry() {
			w.spanStats = append(w.spanStats, s.Stat())
		}
		w.sampler.Add(*s, now)
	}

	var dropped map[uint64]struct{}
	for i := range b.Series {
		s := &b.Series[i]
		if w.filter.DropSeries(s) {
			if dropped == nil {
				dropped = map[uint64]struct{}{}
			}
			dropped[s.ID] = struct{}{}
			continue
		}
		if w.series.ShouldWrite(s.ID, now) {
			w.buf.Series = append(w.buf.Series, *s)
		}
	}
	for _, smp := range b.Samples {
		if _, ok := dropped[smp.SeriesID]; !ok {
			w.buf.Samples = append(w.buf.Samples, smp)
		}
	}
}

func (w *WriterService) Buffered() int {
	return w.buf.Rows() + w.sampler.Pending()
}

// Flush writes dimensions first so fact rows never reference unknown IDs.
// Each step clears its rows only once written, so a retry after a partial
// failure does not duplicate the steps that already succeeded.
func (w *WriterService) Flush(ctx context.Context, now time.Time, final bool) error {
	w.buf.Spans = append(w.buf.Spans, w.sampler.Due(now, final)...)

	steps := []struct {
		name string
		run  func() error
	}{
		{"resources", func() error { return saveAndClear(ctx, &w.buf.Resources, w.repo.SaveResources) }},
		{"series", func() error { return saveAndClear(ctx, &w.buf.Series, w.repo.SaveSeries) }},
		{"span stats", func() error { return saveAndClear(ctx, &w.spanStats, w.repo.SaveSpanStats) }},
		{"spans", func() error { return saveAndClear(ctx, &w.buf.Spans, w.repo.SaveSpans) }},
		{"logs", func() error { return saveAndClear(ctx, &w.buf.Logs, w.repo.SaveLogs) }},
		{"samples", func() error { return saveAndClear(ctx, &w.buf.Samples, w.repo.SaveSamples) }},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			return fmt.Errorf("save %s: %w", s.name, err)
		}
	}
	return nil
}

func saveAndClear[T any](ctx context.Context, rows *[]T, save func(context.Context, []T) error) error {
	if len(*rows) == 0 {
		return nil
	}
	if err := save(ctx, *rows); err != nil {
		return err
	}
	*rows = (*rows)[:0]
	return nil
}
