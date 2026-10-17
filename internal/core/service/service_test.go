package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
	"github.com/b1ll1eee/vindice-observability/internal/core/policy"
)

type fakePublisher struct {
	err   error
	calls int
}

func (f *fakePublisher) Publish(context.Context, domain.Signal, []byte) error {
	f.calls++
	return f.err
}

func TestIngestService(t *testing.T) {
	pub := &fakePublisher{}
	s := NewIngestService(pub)
	ctx := context.Background()

	if err := s.Ingest(ctx, "bogus", []byte{1}); !errors.Is(err, domain.ErrInvalidSignal) {
		t.Errorf("invalid signal: %v", err)
	}
	if err := s.Ingest(ctx, domain.SignalLogs, nil); err != nil || pub.calls != 0 {
		t.Errorf("empty payload should be a no-op: %v, %d calls", err, pub.calls)
	}
	pub.err = errors.New("broker down")
	if err := s.Ingest(ctx, domain.SignalLogs, []byte{1}); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("publish failure should map to ErrUnavailable: %v", err)
	}
}

// fakeRepo records saved rows and can fail one step once.
type fakeRepo struct {
	failSpansOnce bool
	resources     int
	logs          int
	spans         int
	stats         int
	series        int
	samples       int
}

func (f *fakeRepo) SaveResources(_ context.Context, r []domain.Resource) error {
	f.resources += len(r)
	return nil
}
func (f *fakeRepo) SaveLogs(_ context.Context, r []domain.LogRow) error { f.logs += len(r); return nil }
func (f *fakeRepo) SaveSpans(_ context.Context, r []domain.SpanRow) error {
	if f.failSpansOnce {
		f.failSpansOnce = false
		return errors.New("clickhouse down")
	}
	f.spans += len(r)
	return nil
}
func (f *fakeRepo) SaveSpanStats(_ context.Context, r []domain.SpanStat) error {
	f.stats += len(r)
	return nil
}
func (f *fakeRepo) SaveSeries(_ context.Context, r []domain.Series) error {
	f.series += len(r)
	return nil
}
func (f *fakeRepo) SaveSamples(_ context.Context, r []domain.Sample) error {
	f.samples += len(r)
	return nil
}

func newWriter(t *testing.T, repo *fakeRepo, rules []policy.Rule, rate float64) *WriterService {
	t.Helper()
	filter, err := policy.NewFilter(rules)
	if err != nil {
		t.Fatal(err)
	}
	return NewWriterService(WriterDeps{
		Repo:      repo,
		Filter:    filter,
		Sampler:   policy.NewSampler(policy.SamplerConfig{Rate: rate, Wait: time.Second}),
		Resources: policy.NewSeen(time.Hour, 100),
		Series:    policy.NewSeen(time.Hour, 100),
	})
}

func TestWriterRetryDoesNotDuplicateWrittenSteps(t *testing.T) {
	repo := &fakeRepo{failSpansOnce: true}
	w := newWriter(t, repo, nil, 1)
	now := time.Now()
	w.Add(domain.Batch{
		Resources: []domain.Resource{{ID: 1}},
		Logs:      []domain.LogRow{{}, {}},
		Spans:     []domain.SpanRow{{Kind: domain.KindServer}},
	}, now)

	if err := w.Flush(context.Background(), now, false); err == nil {
		t.Fatal("expected first flush to fail")
	}
	if err := w.Flush(context.Background(), now, false); err != nil {
		t.Fatal(err)
	}
	if repo.resources != 1 || repo.stats != 1 || repo.spans != 1 || repo.logs != 2 {
		t.Errorf("rows written: %+v", repo)
	}
	if w.Buffered() != 0 {
		t.Errorf("buffer not empty: %d", w.Buffered())
	}
}

func TestWriterAppliesPolicies(t *testing.T) {
	repo := &fakeRepo{}
	w := newWriter(t, repo, []policy.Rule{
		{Signal: domain.SignalTraces, Name: "healthz"},
		{Signal: domain.SignalMetrics, Name: "^noise$"},
	}, 0) // rate 0: only error/slow traces are kept
	now := time.Now()
	w.Add(domain.Batch{
		Resources: []domain.Resource{{ID: 1}, {ID: 1}},
		Spans: []domain.SpanRow{
			{Name: "healthz", Kind: domain.KindServer},
			{Name: "GET /a", Kind: domain.KindServer, StatusCode: domain.StatusOk},
			{Name: "GET /b", Kind: domain.KindServer, StatusCode: domain.StatusError, TraceID: [16]byte{9}},
		},
		Series:  []domain.Series{{ID: 1, MetricName: "noise"}, {ID: 2, MetricName: "up"}},
		Samples: []domain.Sample{{SeriesID: 1}, {SeriesID: 2}, {SeriesID: 2}},
	}, now)
	w.Add(domain.Batch{Resources: []domain.Resource{{ID: 1}}}, now)

	if err := w.Flush(context.Background(), now, true); err != nil {
		t.Fatal(err)
	}
	if repo.resources != 1 {
		t.Errorf("resource should be deduplicated, written %d", repo.resources)
	}
	if repo.stats != 2 {
		t.Errorf("RED stats come from every non-dropped entry span before sampling, got %d", repo.stats)
	}
	if repo.spans != 1 {
		t.Errorf("only the error trace should survive sampling, got %d", repo.spans)
	}
	if repo.series != 1 || repo.samples != 2 {
		t.Errorf("dropped series and its samples should be gone: series=%d samples=%d", repo.series, repo.samples)
	}
}

// Flushing must let go of the rows it wrote: an emptied slice that still
// points at millions of old rows keeps their strings and maps alive.
func TestFlushReleasesWrittenRows(t *testing.T) {
	w := newWriter(t, &fakeRepo{}, nil, 1)

	small := domain.Batch{Logs: make([]domain.LogRow, 100)}
	for i := range small.Logs {
		small.Logs[i] = domain.LogRow{Body: "small", Attributes: map[string]string{"k": "v"}}
	}
	w.Add(small, time.Now())
	if err := w.Flush(context.Background(), time.Now(), false); err != nil {
		t.Fatal(err)
	}
	for i, r := range w.buf.Logs[:cap(w.buf.Logs)] {
		if r.Body != "" || r.Attributes != nil {
			t.Fatalf("row %d still referenced after flush: %+v", i, r)
		}
	}

	big := domain.Batch{Logs: make([]domain.LogRow, 500_000)}
	w.Add(big, time.Now())
	if err := w.Flush(context.Background(), time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if c := cap(w.buf.Logs); c > maxRetainedRows {
		t.Errorf("flush kept a buffer for %d rows; want at most %d", c, maxRetainedRows)
	}
}
