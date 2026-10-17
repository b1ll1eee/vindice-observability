package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

type fakeQuerier struct {
	gotLog   domain.LogQuery
	gotTrace domain.TraceQuery
	page     domain.LogPage
	trace    domain.Trace
	err      error
}

func (f *fakeQuerier) Services(context.Context, domain.TimeRange) ([]domain.ServiceSummary, error) {
	return []domain.ServiceSummary{{Name: "orders", Requests: 4, Errors: 1, Latency: domain.Latency{P95: 12.5}}}, f.err
}
func (f *fakeQuerier) Operations(context.Context, string, domain.TimeRange) ([]domain.OperationSummary, error) {
	return nil, f.err
}
func (f *fakeQuerier) SearchLogs(_ context.Context, q domain.LogQuery) (domain.LogPage, error) {
	f.gotLog = q
	return f.page, f.err
}
func (f *fakeQuerier) LogHistogram(_ context.Context, q domain.LogQuery) (domain.Histogram, error) {
	f.gotLog = q
	return domain.Histogram{Step: time.Minute}, f.err
}
func (f *fakeQuerier) SearchTraces(_ context.Context, q domain.TraceQuery) ([]domain.TraceSummary, error) {
	f.gotTrace = q
	return nil, f.err
}
func (f *fakeQuerier) Trace(context.Context, [16]byte) (domain.Trace, error) { return f.trace, f.err }

func get(t *testing.T, q *fakeQuerier, url string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	h := New(q, http.NotFoundHandler(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func TestLogsParsesFilters(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := &fakeQuerier{page: domain.LogPage{
		Entries: []domain.LogEntry{{Timestamp: now, SeverityNumber: 17, TraceID: [16]byte{0xab}}},
		Next:    now,
	}}
	id := "ab000000000000000000000000000000"
	url := fmt.Sprintf("/api/v1/logs?from=1000&to=2000&service=api&severity=13&q=db+timeout&trace_id=%s&before=42&limit=5", id)
	rec, body := get(t, q, url)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	g := q.gotLog
	if g.Range.From.UnixMilli() != 1000 || g.Range.To.UnixMilli() != 2000 || g.Service != "api" ||
		g.MinSeverity != 13 || g.Text != "db timeout" || g.TraceID == nil || g.TraceID[0] != 0xab ||
		g.Before.UnixNano() != 42 || g.Limit != 5 {
		t.Errorf("query = %+v", g)
	}
	logs := body["logs"].([]any)
	first := logs[0].(map[string]any)
	if first["level"] != "error" || first["trace_id"] != id {
		t.Errorf("log dto = %v", first)
	}
	if _, ok := first["span_id"]; ok {
		t.Error("all-zero span id should be omitted")
	}
	if body["next"] != fmt.Sprint(now.UnixNano()) {
		t.Errorf("next = %v", body["next"])
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		url  string
		err  error
		code int
	}{
		{"/api/v1/logs?from=abc", nil, http.StatusBadRequest},
		{"/api/v1/logs?trace_id=xyz", nil, http.StatusBadRequest},
		{"/api/v1/traces/not-hex", nil, http.StatusBadRequest},
		{"/api/v1/traces/00000000000000000000000000000001", domain.ErrNotFound, http.StatusNotFound},
		{"/api/v1/services", domain.ErrInvalidQuery, http.StatusBadRequest},
		{"/api/v1/services", fmt.Errorf("boom"), http.StatusInternalServerError},
		{"/api/v1/nope", nil, http.StatusNotFound},
	}
	for _, c := range cases {
		rec, body := get(t, &fakeQuerier{err: c.err}, c.url)
		if rec.Code != c.code || body["error"] == nil {
			t.Errorf("%s (%v): code %d body %s", c.url, c.err, rec.Code, rec.Body)
		}
	}
}

func TestServicesDTO(t *testing.T) {
	h := New(&fakeQuerier{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/services", nil))
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got[0]["error_rate"] != 0.25 || got[0]["p95_ms"] != 12.5 {
		t.Errorf("service dto = %v", got[0])
	}
}

func TestTraceDTO(t *testing.T) {
	start := time.Unix(100, 0)
	q := &fakeQuerier{trace: domain.Trace{
		ID: [16]byte{1},
		Spans: []domain.SpanRow{
			{Timestamp: start, SpanID: [8]byte{1}, ServiceName: "api", DurationNs: uint64(10 * time.Millisecond),
				HTTPRoute: "/orders", HTTPStatus: 500, ResourceID: 9},
			{Timestamp: start.Add(2 * time.Millisecond), SpanID: [8]byte{2}, ParentSpanID: [8]byte{1},
				ServiceName: "db", DurationNs: uint64(15 * time.Millisecond),
				EventNames: []string{"retry"}, EventTimes: []time.Time{start.Add(3 * time.Millisecond)},
				EventAttrs: []map[string]string{nil}},
		},
		Resources: map[uint64]map[string]string{9: {"host.name": "n1"}},
	}}
	rec, body := get(t, q, "/api/v1/traces/01000000000000000000000000000000")
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	if body["duration_ms"] != 17.0 {
		t.Errorf("duration should run to the latest span end: %v", body["duration_ms"])
	}
	spans := body["spans"].([]any)
	root, child := spans[0].(map[string]any), spans[1].(map[string]any)
	attrs := root["attributes"].(map[string]any)
	if attrs["http.route"] != "/orders" || attrs["http.response.status_code"] != "500" {
		t.Errorf("promoted attributes not restored: %v", attrs)
	}
	if root["resource"].(map[string]any)["host.name"] != "n1" {
		t.Errorf("resource = %v", root["resource"])
	}
	if child["offset_ms"] != 2.0 || child["parent_id"] != "0100000000000000" {
		t.Errorf("child = %v", child)
	}
	if ev := child["events"].([]any)[0].(map[string]any); ev["offset_ms"] != 3.0 {
		t.Errorf("event = %v", ev)
	}
}
