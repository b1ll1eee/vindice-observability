package otlphttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

type fakeIngestor struct {
	err      error
	sig      domain.Signal
	payloads [][]byte
}

func (f *fakeIngestor) Ingest(_ context.Context, sig domain.Signal, payload []byte) error {
	f.sig = sig
	f.payloads = append(f.payloads, payload)
	return f.err
}

func traceReq(t *testing.T) []byte {
	t.Helper()
	b, err := proto.Marshal(&coltrace.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{Name: "x"}}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func do(h http.Handler, path, ct, enc string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if enc != "" {
		req.Header.Set("Content-Encoding", enc)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newHandler(ing *fakeIngestor, max int64) http.Handler {
	return New(ing, max, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestProtobuf(t *testing.T) {
	ing := &fakeIngestor{}
	rec := do(newHandler(ing, 1<<20), "/v1/traces", contentProto, "", traceReq(t))
	if rec.Code != http.StatusOK || ing.sig != domain.SignalTraces || len(ing.payloads) != 1 {
		t.Fatalf("code=%d sig=%s calls=%d body=%s", rec.Code, ing.sig, len(ing.payloads), rec.Body)
	}
	if rec.Header().Get("Content-Type") != contentProto {
		t.Errorf("response content type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestGzip(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(traceReq(t))
	_ = gz.Close()

	ing := &fakeIngestor{}
	if rec := do(newHandler(ing, 1<<20), "/v1/traces", contentProto, "gzip", buf.Bytes()); rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestJSONHexIDs(t *testing.T) {
	body := `{"resourceSpans":[{"scopeSpans":[{"spans":[{
		"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b174",
		"name":"GET /","kind":2,"startTimeUnixNano":"1000","endTimeUnixNano":"2000"}]}]}]}`
	ing := &fakeIngestor{}
	rec := do(newHandler(ing, 1<<20), "/v1/traces", "application/json; charset=utf-8", "", []byte(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}

	// The queue must hold protobuf with the IDs decoded from hex.
	var req coltrace.ExportTraceServiceRequest
	if err := proto.Unmarshal(ing.payloads[0], &req); err != nil {
		t.Fatal(err)
	}
	sp := req.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()[0]
	if got := sp.GetTraceId(); len(got) != 16 || got[0] != 0x5b || got[15] != 0x0c {
		t.Errorf("trace id = %x", got)
	}
	if sp.GetEndTimeUnixNano() != 2000 {
		t.Errorf("end time = %d", sp.GetEndTimeUnixNano())
	}
}

func TestErrors(t *testing.T) {
	ing := &fakeIngestor{}
	h := newHandler(ing, 64)

	if rec := do(h, "/v1/logs", "text/plain", "", []byte("x")); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("content type: %d", rec.Code)
	}
	if rec := do(h, "/v1/logs", contentProto, "", []byte{0xff, 0xff}); rec.Code != http.StatusBadRequest {
		t.Errorf("garbage: %d", rec.Code)
	}
	if rec := do(h, "/v1/logs", contentProto, "", bytes.Repeat([]byte{0}, 100)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d", rec.Code)
	}
	if rec := do(h, "/v1/logs", contentProto, "br", []byte{}); rec.Code != http.StatusBadRequest {
		t.Errorf("encoding: %d", rec.Code)
	}

	ing.err = domain.ErrUnavailable
	rec := do(newHandler(ing, 1<<20), "/v1/traces", contentProto, "", traceReq(t))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Errorf("unavailable: %d %v", rec.Code, rec.Header())
	}
}

func TestEmptyRequestIsNotQueued(t *testing.T) {
	ing := &fakeIngestor{}
	rec := do(newHandler(ing, 1<<20), "/v1/metrics", contentJSON, "", []byte(`{}`))
	if rec.Code != http.StatusOK || len(ing.payloads) != 0 {
		t.Errorf("code=%d calls=%d", rec.Code, len(ing.payloads))
	}
	if !strings.HasPrefix(rec.Body.String(), "{") {
		t.Errorf("json response = %q", rec.Body)
	}
}
