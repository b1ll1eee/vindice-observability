package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

const (
	defaultRange      = time.Hour
	maxRange          = 90 * 24 * time.Hour
	defaultLogLimit   = 100
	maxLogLimit       = 1000
	defaultTraceLimit = 50
	maxTraceLimit     = 500
	histogramBuckets  = 60
	// Logs of a trace are searched slightly beyond its spans, since clocks
	// and buffering skew log timestamps.
	traceLogMargin = time.Minute
)

// niceSteps are the histogram bucket sizes, smallest first.
var niceSteps = []time.Duration{
	time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second,
	time.Minute, 5 * time.Minute, 10 * time.Minute, 30 * time.Minute,
	time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

// QueryService holds the read-side rules: defaults and limits, when the
// pre-aggregated rollups can answer, pagination and trace assembly.
type QueryService struct {
	reader port.TelemetryReader
	now    func() time.Time
}

var _ port.Querier = (*QueryService)(nil)

func NewQueryService(reader port.TelemetryReader) *QueryService {
	return &QueryService{reader: reader, now: time.Now}
}

func (s *QueryService) Services(ctx context.Context, r domain.TimeRange) ([]domain.ServiceSummary, error) {
	r, err := s.normalize(r)
	if err != nil {
		return nil, err
	}
	red, err := s.reader.ServiceRED(ctx, r)
	if err != nil {
		return nil, err
	}
	counts, err := s.reader.ServiceLogCounts(ctx, r)
	if err != nil {
		return nil, err
	}

	// A service may only emit logs, or only spans; list it either way.
	byName := make(map[string]domain.ServiceSummary, len(red)+len(counts))
	for _, svc := range red {
		byName[svc.Name] = svc
	}
	for name, c := range counts {
		svc := byName[name]
		svc.Name, svc.Logs, svc.LogErrors = name, c.Total, c.Errors
		byName[name] = svc
	}
	out := make([]domain.ServiceSummary, 0, len(byName))
	for _, svc := range byName {
		out = append(out, svc)
	}
	slices.SortFunc(out, func(a, b domain.ServiceSummary) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (s *QueryService) Operations(ctx context.Context, service string, r domain.TimeRange) ([]domain.OperationSummary, error) {
	if service == "" {
		return nil, fmt.Errorf("%w: service is required", domain.ErrInvalidQuery)
	}
	r, err := s.normalize(r)
	if err != nil {
		return nil, err
	}
	return s.reader.OperationRED(ctx, service, r)
}

func (s *QueryService) SearchLogs(ctx context.Context, q domain.LogQuery) (domain.LogPage, error) {
	var err error
	if q.Range, err = s.normalize(q.Range); err != nil {
		return domain.LogPage{}, err
	}
	q.Limit = clampLimit(q.Limit, defaultLogLimit, maxLogLimit)
	limit := q.Limit
	q.Limit++ // one extra row tells whether another page exists

	entries, err := s.reader.SearchLogs(ctx, q)
	if err != nil {
		return domain.LogPage{}, err
	}
	page := domain.LogPage{Entries: entries}
	if len(entries) > limit {
		page.Entries = entries[:limit]
		page.Next = entries[limit-1].Timestamp
	}
	return page, nil
}

func (s *QueryService) LogHistogram(ctx context.Context, q domain.LogQuery) (domain.Histogram, error) {
	var err error
	if q.Range, err = s.normalize(q.Range); err != nil {
		return domain.Histogram{}, err
	}
	step := pickStep(q.Range.To.Sub(q.Range.From))
	rollup := !q.HasContentFilter()
	if rollup {
		// The rollup is per minute; finer buckets would come out empty.
		step = max(step, time.Minute)
	}

	got, err := s.reader.LogHistogram(ctx, q, step, rollup)
	if err != nil {
		return domain.Histogram{}, err
	}
	return domain.Histogram{Step: step, Buckets: fillBuckets(q.Range, step, got)}, nil
}

func (s *QueryService) SearchTraces(ctx context.Context, q domain.TraceQuery) ([]domain.TraceSummary, error) {
	var err error
	if q.Range, err = s.normalize(q.Range); err != nil {
		return nil, err
	}
	q.Limit = clampLimit(q.Limit, defaultTraceLimit, maxTraceLimit)
	return s.reader.SearchTraces(ctx, q)
}

func (s *QueryService) Trace(ctx context.Context, id [16]byte) (domain.Trace, error) {
	window, ok, err := s.reader.TraceWindow(ctx, id)
	if err != nil {
		return domain.Trace{}, err
	}
	if !ok {
		return domain.Trace{}, domain.ErrNotFound
	}
	spans, err := s.reader.TraceSpans(ctx, id, window)
	if err != nil {
		return domain.Trace{}, err
	}
	if len(spans) == 0 {
		return domain.Trace{}, domain.ErrNotFound
	}

	end := window.To
	ids := make([]uint64, 0, 4)
	for _, sp := range spans {
		end = maxTime(end, sp.Timestamp.Add(time.Duration(sp.DurationNs)))
		if !slices.Contains(ids, sp.ResourceID) {
			ids = append(ids, sp.ResourceID)
		}
	}
	logs, err := s.reader.TraceLogs(ctx, id, domain.TimeRange{
		From: window.From.Add(-traceLogMargin),
		To:   end.Add(traceLogMargin),
	})
	if err != nil {
		return domain.Trace{}, err
	}
	resources, err := s.reader.Resources(ctx, ids)
	if err != nil {
		return domain.Trace{}, err
	}
	return domain.Trace{ID: id, Spans: spans, Logs: logs, Resources: resources}, nil
}

// normalize applies the default range and rejects empty or oversized ones.
func (s *QueryService) normalize(r domain.TimeRange) (domain.TimeRange, error) {
	if r.To.IsZero() {
		r.To = s.now()
	}
	if r.From.IsZero() {
		r.From = r.To.Add(-defaultRange)
	}
	switch {
	case !r.From.Before(r.To):
		return r, fmt.Errorf("%w: from must be before to", domain.ErrInvalidQuery)
	case r.To.Sub(r.From) > maxRange:
		return r, fmt.Errorf("%w: range is longer than %s", domain.ErrInvalidQuery, maxRange)
	}
	return r, nil
}

func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	return min(n, max)
}

func pickStep(span time.Duration) time.Duration {
	target := span / histogramBuckets
	for _, s := range niceSteps {
		if s >= target {
			return s
		}
	}
	return niceSteps[len(niceSteps)-1]
}

// fillBuckets returns one bucket per step across the range, zero where the
// reader had no rows, so charts get an evenly spaced series.
func fillBuckets(r domain.TimeRange, step time.Duration, got []domain.HistogramBucket) []domain.HistogramBucket {
	byTime := make(map[int64]domain.HistogramBucket, len(got))
	for _, b := range got {
		byTime[b.Time.Unix()] = b
	}
	var out []domain.HistogramBucket
	for t := r.From.Truncate(step); t.Before(r.To); t = t.Add(step) {
		b, ok := byTime[t.Unix()]
		if !ok {
			b = domain.HistogramBucket{Time: t}
		}
		out = append(out, b)
	}
	return out
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
