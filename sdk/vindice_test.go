package vindice

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

// fakeVindice records OTLP/HTTP requests.
type fakeVindice struct {
	mu     sync.Mutex
	auth   []string
	bodies map[string][][]byte
}

func (f *fakeVindice) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var rd io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		rd = gz
	}
	b, _ := io.ReadAll(rd)
	f.mu.Lock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.bodies[r.URL.Path] = append(f.bodies[r.URL.Path], b)
	f.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func TestStartSendsAllSignalsWithKey(t *testing.T) {
	fake := &fakeVindice{bodies: map[string][][]byte{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	ctx := context.Background()
	sdk, err := Start(ctx, Config{
		ServiceName: "orders", ServiceVersion: "1.2.3", Environment: "test",
		Endpoint: srv.URL, APIKey: "vdc_test", LogOutput: io.Discard,
		Attributes: map[string]string{"team": "payments"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// A traced request with a log line inside it.
	ctx, span := otel.Tracer("test").Start(ctx, "POST /orders")
	sdk.Logger().InfoContext(ctx, "order created", "order_id", 42)
	span.End()
	counter, _ := otel.Meter("test").Int64Counter("orders_created")
	counter.Add(ctx, 1)

	if err := sdk.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, path := range []string{"/v1/traces", "/v1/logs", "/v1/metrics"} {
		if len(fake.bodies[path]) == 0 {
			t.Errorf("nothing sent to %s", path)
		}
	}
	for _, a := range fake.auth {
		if a != "Bearer vdc_test" {
			t.Errorf("authorization header = %q", a)
		}
	}

	var traces coltrace.ExportTraceServiceRequest
	if err := proto.Unmarshal(fake.bodies["/v1/traces"][0], &traces); err != nil {
		t.Fatal(err)
	}
	res := map[string]string{}
	for _, a := range traces.ResourceSpans[0].Resource.Attributes {
		res[a.Key] = a.Value.GetStringValue()
	}
	for k, want := range map[string]string{
		"service.name": "orders", "service.version": "1.2.3", "deployment.environment.name": "test", "team": "payments",
	} {
		if res[k] != want {
			t.Errorf("resource %s = %q, want %q", k, res[k], want)
		}
	}
	sentSpan := traces.ResourceSpans[0].ScopeSpans[0].Spans[0]

	// The log line carries the span's trace id: that is what links logs to
	// traces in the Vindice UI.
	var logs collogs.ExportLogsServiceRequest
	if err := proto.Unmarshal(fake.bodies["/v1/logs"][0], &logs); err != nil {
		t.Fatal(err)
	}
	rec := logs.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	if rec.Body.GetStringValue() != "order created" || string(rec.TraceId) != string(sentSpan.TraceId) {
		t.Errorf("log = %q, trace %x vs span trace %x", rec.Body.GetStringValue(), rec.TraceId, sentSpan.TraceId)
	}
	if trace.SpanContextFromContext(ctx).TraceID().String() == "" {
		t.Error("no trace in context")
	}
}

func TestHandlerAndTransportPropagate(t *testing.T) {
	fake := &fakeVindice{bodies: map[string][][]byte{}}
	collector := httptest.NewServer(fake)
	defer collector.Close()
	sdk, err := Start(context.Background(), Config{ServiceName: "svc", Endpoint: collector.URL, LogOutput: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Shutdown(context.Background())

	var gotParent string
	backend := httptest.NewServer(Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotParent = r.Header.Get("Traceparent")
	}), "backend"))
	defer backend.Close()

	ctx, span := otel.Tracer("t").Start(context.Background(), "caller")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, backend.URL, nil)
	resp, err := (&http.Client{Transport: Transport(nil), Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	span.End()

	if !strings.Contains(gotParent, span.SpanContext().TraceID().String()) {
		t.Errorf("traceparent %q does not carry trace %s", gotParent, span.SpanContext().TraceID())
	}
}

func TestConfigErrors(t *testing.T) {
	if _, err := Start(context.Background(), Config{}); err == nil {
		t.Error("missing service name accepted")
	}
	if _, err := Start(context.Background(), Config{ServiceName: "x", Endpoint: "ftp://host"}); err == nil {
		t.Error("bad endpoint accepted")
	}
	for in, want := range map[string]struct {
		host     string
		insecure bool
	}{
		"http://vindice:4318": {"vindice:4318", true},
		"https://obs.example": {"obs.example", false},
		"vindice:4318":        {"vindice:4318", true},
	} {
		h, ins, err := parseEndpoint(in)
		if err != nil || h != want.host || ins != want.insecure {
			t.Errorf("parseEndpoint(%q) = %q %v %v", in, h, ins, err)
		}
	}
}
