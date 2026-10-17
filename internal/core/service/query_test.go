package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

// fakeReader records what the service asked for and returns canned rows.
type fakeReader struct {
	red       []domain.ServiceSummary
	logCounts map[string]domain.LogCount
	logs      []domain.LogEntry
	buckets   []domain.HistogramBucket
	window    domain.TimeRange
	known     bool
	spans     []domain.SpanRow

	gotLogQuery  domain.LogQuery
	gotStep      time.Duration
	gotRollup    bool
	gotLogsRange domain.TimeRange
	gotResIDs    []uint64
}

func (f *fakeReader) ServiceRED(context.Context, domain.TimeRange) ([]domain.ServiceSummary, error) {
	return f.red, nil
}
func (f *fakeReader) ServiceLogCounts(context.Context, domain.TimeRange) (map[string]domain.LogCount, error) {
	return f.logCounts, nil
}
func (f *fakeReader) OperationRED(context.Context, string, domain.TimeRange) ([]domain.OperationSummary, error) {
	return nil, nil
}
func (f *fakeReader) SearchLogs(_ context.Context, q domain.LogQuery) ([]domain.LogEntry, error) {
	f.gotLogQuery = q
	return f.logs[:min(len(f.logs), q.Limit)], nil
}
func (f *fakeReader) LogHistogram(_ context.Context, q domain.LogQuery, step time.Duration, rollup bool) ([]domain.HistogramBucket, error) {
	f.gotLogQuery, f.gotStep, f.gotRollup = q, step, rollup
	return f.buckets, nil
}
func (f *fakeReader) SearchTraces(context.Context, domain.TraceQuery) ([]domain.TraceSummary, error) {
	return nil, nil
}
func (f *fakeReader) TraceWindow(context.Context, [16]byte) (domain.TimeRange, bool, error) {
	return f.window, f.known, nil
}
func (f *fakeReader) TraceSpans(context.Context, [16]byte, domain.TimeRange) ([]domain.SpanRow, error) {
	return f.spans, nil
}
func (f *fakeReader) TraceLogs(_ context.Context, _ [16]byte, r domain.TimeRange) ([]domain.LogEntry, error) {
	f.gotLogsRange = r
	return nil, nil
}
func (f *fakeReader) Resources(_ context.Context, ids []uint64) (map[uint64]map[string]string, error) {
	f.gotResIDs = ids
	return map[uint64]map[string]string{}, nil
}

var t0 = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func newQuery(r *fakeReader) *QueryService {
	s := NewQueryService(r)
	s.now = func() time.Time { return t0 }
	return s
}

func TestNormalizeRange(t *testing.T) {
	s := newQuery(&fakeReader{})
	r, err := s.normalize(domain.TimeRange{})
	if err != nil || !r.To.Equal(t0) || r.To.Sub(r.From) != time.Hour {
		t.Errorf("default range = %v, %v", r, err)
	}
	if _, err := s.normalize(domain.TimeRange{From: t0, To: t0}); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("empty range accepted: %v", err)
	}
	if _, err := s.normalize(domain.TimeRange{From: t0.Add(-100 * 24 * time.Hour), To: t0}); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("oversized range accepted: %v", err)
	}
}

func TestServicesMergesLogOnlyServices(t *testing.T) {
	s := newQuery(&fakeReader{
		red:       []domain.ServiceSummary{{Name: "orders", Requests: 10}},
		logCounts: map[string]domain.LogCount{"orders": {Total: 5, Errors: 1}, "cron": {Total: 3}},
	})
	got, err := s.Services(context.Background(), domain.TimeRange{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "cron" || got[1].Name != "orders" {
		t.Fatalf("services = %+v", got)
	}
	if got[1].Requests != 10 || got[1].Logs != 5 || got[1].LogErrors != 1 || got[0].Logs != 3 {
		t.Errorf("merged = %+v", got)
	}
}

func TestSearchLogsPagination(t *testing.T) {
	logs := make([]domain.LogEntry, 5)
	for i := range logs {
		logs[i].Timestamp = t0.Add(-time.Duration(i) * time.Second)
	}
	r := &fakeReader{logs: logs}
	page, err := newQuery(r).SearchLogs(context.Background(), domain.LogQuery{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.gotLogQuery.Limit != 4 {
		t.Errorf("reader asked for %d rows, want limit+1", r.gotLogQuery.Limit)
	}
	if len(page.Entries) != 3 || !page.Next.Equal(logs[2].Timestamp) {
		t.Errorf("page = %d entries, next %v", len(page.Entries), page.Next)
	}

	r.logs = logs[:2]
	page, _ = newQuery(r).SearchLogs(context.Background(), domain.LogQuery{Limit: 3})
	if !page.Next.IsZero() {
		t.Error("last page should have no cursor")
	}
}

func TestHistogramUsesRollupOnlyWithoutContentFilter(t *testing.T) {
	r := &fakeReader{buckets: []domain.HistogramBucket{{Time: t0.Add(-30 * time.Minute), Errors: 2}}}
	s := newQuery(r)

	h, err := s.LogHistogram(context.Background(), domain.LogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.gotRollup || r.gotStep != time.Minute {
		t.Errorf("1h, no filter: rollup=%v step=%v", r.gotRollup, r.gotStep)
	}
	if len(h.Buckets) != 60 {
		t.Errorf("buckets = %d, want 60 (filled)", len(h.Buckets))
	}
	if h.Buckets[30].Errors != 2 {
		t.Errorf("reader bucket not placed: %+v", h.Buckets[30])
	}

	_, _ = s.LogHistogram(context.Background(), domain.LogQuery{
		Text:  "timeout",
		Range: domain.TimeRange{From: t0.Add(-15 * time.Minute), To: t0},
	})
	if r.gotRollup || r.gotStep != 30*time.Second {
		t.Errorf("15m with text: rollup=%v step=%v", r.gotRollup, r.gotStep)
	}
}

func TestTrace(t *testing.T) {
	r := &fakeReader{}
	if _, err := newQuery(r).Trace(context.Background(), [16]byte{1}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown trace: %v", err)
	}

	r = &fakeReader{
		known:  true,
		window: domain.TimeRange{From: t0, To: t0.Add(time.Second)},
		spans: []domain.SpanRow{
			{Timestamp: t0, DurationNs: uint64(5 * time.Second), ResourceID: 7},
			{Timestamp: t0.Add(time.Second), ResourceID: 7},
		},
	}
	tr, err := newQuery(r).Trace(context.Background(), [16]byte{1})
	if err != nil || len(tr.Spans) != 2 {
		t.Fatalf("trace: %v %+v", err, tr)
	}
	// Logs are searched from a minute before the first span to a minute after
	// the latest span end (t0+5s), not just the start window.
	if !r.gotLogsRange.From.Equal(t0.Add(-time.Minute)) || !r.gotLogsRange.To.Equal(t0.Add(5*time.Second+time.Minute)) {
		t.Errorf("logs range = %+v", r.gotLogsRange)
	}
	if len(r.gotResIDs) != 1 || r.gotResIDs[0] != 7 {
		t.Errorf("resource ids = %v", r.gotResIDs)
	}
}

func TestPickStep(t *testing.T) {
	for span, want := range map[time.Duration]time.Duration{
		15 * time.Minute:   30 * time.Second,
		time.Hour:          time.Minute,
		24 * time.Hour:     30 * time.Minute,
		7 * 24 * time.Hour: 3 * time.Hour,
	} {
		if got := pickStep(span); got != want {
			t.Errorf("pickStep(%v) = %v, want %v", span, got, want)
		}
	}
}
