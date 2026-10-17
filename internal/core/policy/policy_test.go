package policy

import (
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

func TestFilter(t *testing.T) {
	f, err := NewFilter([]Rule{
		{Signal: domain.SignalTraces, Name: `^GET /(healthz|metrics)$`},
		{Signal: domain.SignalLogs, Service: "api", MaxSeverity: 8},
		{Signal: domain.SignalLogs, Body: "noisy", Attribute: "env", Value: "dev"},
		{Signal: domain.SignalMetrics, Name: `^go_gc_`},
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		drop bool
	}{
		{"healthz span", f.DropSpan(&domain.SpanRow{Name: "GET /healthz"})},
		{"debug log of api", f.DropLog(&domain.LogRow{ServiceName: "api", SeverityNumber: 5})},
		{"gc metric", f.DropSeries(&domain.Series{MetricName: "go_gc_duration"})},
		{"noisy dev log", f.DropLog(&domain.LogRow{Body: "very noisy", Attributes: map[string]string{"env": "dev"}})},
	}
	for _, c := range cases {
		if !c.drop {
			t.Errorf("%s should be dropped", c.name)
		}
	}

	keep := []struct {
		name string
		drop bool
	}{
		{"users span", f.DropSpan(&domain.SpanRow{Name: "GET /users"})},
		{"error log of api", f.DropLog(&domain.LogRow{ServiceName: "api", SeverityNumber: 17})},
		{"unknown-severity log of api", f.DropLog(&domain.LogRow{ServiceName: "api"})},
		{"debug log of worker", f.DropLog(&domain.LogRow{ServiceName: "worker", SeverityNumber: 5})},
		{"noisy prod log", f.DropLog(&domain.LogRow{Body: "very noisy", Attributes: map[string]string{"env": "prod"}})},
	}
	for _, c := range keep {
		if c.drop {
			t.Errorf("%s should be kept", c.name)
		}
	}
}

func TestFilterRejectsBadRules(t *testing.T) {
	if _, err := NewFilter([]Rule{{Signal: "events"}}); err == nil {
		t.Error("unknown signal accepted")
	}
	if _, err := NewFilter([]Rule{{Signal: domain.SignalTraces, Name: "("}}); err == nil {
		t.Error("bad regex accepted")
	}
}

func TestPromoteSpanAttributes(t *testing.T) {
	s := &domain.SpanRow{Attributes: map[string]string{
		"http.method": "GET", "http.route": "/users/:id", "http.status_code": "404", "user.id": "7",
	}}
	PromoteSpanAttributes(s)
	if s.HTTPMethod != "GET" || s.HTTPRoute != "/users/:id" || s.HTTPStatus != 404 {
		t.Errorf("promoted: %+v", s)
	}
	if len(s.Attributes) != 1 || s.Attributes["user.id"] != "7" {
		t.Errorf("promoted keys should be removed, got %v", s.Attributes)
	}
}

func span(trace byte, low uint64, opts ...func(*domain.SpanRow)) domain.SpanRow {
	s := domain.SpanRow{StatusCode: domain.StatusOk, DurationNs: uint64(time.Millisecond)}
	s.TraceID[0] = trace
	for i := range 8 {
		s.TraceID[15-i] = byte(low >> (8 * i))
	}
	for _, o := range opts {
		o(&s)
	}
	return s
}

func TestTailSampler(t *testing.T) {
	s := NewSampler(SamplerConfig{Rate: 0.5, Slow: time.Second, Wait: 10 * time.Second})
	t0 := time.Unix(0, 0)
	const low, high = 1, ^uint64(0) // inside / outside the 50% rate

	s.Add(span(1, low), t0)  // sampled by ID
	s.Add(span(2, high), t0) // normal: dropped
	s.Add(span(3, high, func(r *domain.SpanRow) { r.StatusCode = domain.StatusError }), t0)
	s.Add(span(3, high), t0) // same trace as the error: kept with it
	s.Add(span(4, high, func(r *domain.SpanRow) { r.DurationNs = uint64(2 * time.Second) }), t0)

	if got := s.Due(t0.Add(5*time.Second), false); len(got) != 0 {
		t.Fatalf("decided before wait: %d spans", len(got))
	}
	if s.Pending() != 5 {
		t.Fatalf("pending = %d", s.Pending())
	}
	got := s.Due(t0.Add(11*time.Second), false)
	kept := map[byte]int{}
	for _, sp := range got {
		kept[sp.TraceID[0]]++
	}
	if kept[1] != 1 || kept[2] != 0 || kept[3] != 2 || kept[4] != 1 {
		t.Errorf("kept per trace = %v", kept)
	}

	// A late span follows the earlier decision.
	s.Add(span(2, high), t0.Add(12*time.Second))
	s.Add(span(3, high), t0.Add(12*time.Second))
	late := s.Due(t0.Add(12*time.Second), false)
	if len(late) != 1 || late[0].TraceID[0] != 3 {
		t.Errorf("late spans = %v", late)
	}
}

func TestSamplerForceAndKeepAll(t *testing.T) {
	s := NewSampler(SamplerConfig{Rate: 0, Wait: time.Hour})
	s.Add(span(1, 1, func(r *domain.SpanRow) { r.StatusCode = domain.StatusError }), time.Unix(0, 0))
	if got := s.Due(time.Unix(1, 0), true); len(got) != 1 {
		t.Errorf("force should decide pending traces, got %d", len(got))
	}

	all := NewSampler(SamplerConfig{Rate: 1})
	all.Add(span(1, ^uint64(0)), time.Unix(0, 0))
	if got := all.Due(time.Unix(0, 0), false); len(got) != 1 {
		t.Errorf("rate 1 should keep every span, got %d", len(got))
	}
}

func TestSeen(t *testing.T) {
	s := NewSeen(time.Minute, 2)
	t0 := time.Unix(0, 0)
	if !s.ShouldWrite(1, t0) || s.ShouldWrite(1, t0.Add(time.Second)) {
		t.Error("second write within ttl should be skipped")
	}
	if !s.ShouldWrite(1, t0.Add(2*time.Minute)) {
		t.Error("write after ttl should refresh")
	}
}
