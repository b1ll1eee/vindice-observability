package promwrite

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/snappy"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

// Minimal encoder for prometheus.WriteRequest, mirroring the decoder.

func encLabel(name, value string) []byte {
	var b []byte
	b = protowire.AppendTag(b, 1, protowire.BytesType)
	b = protowire.AppendString(b, name)
	b = protowire.AppendTag(b, 2, protowire.BytesType)
	return protowire.AppendString(b, value)
}

func encSample(v float64, ts int64) []byte {
	var b []byte
	b = protowire.AppendTag(b, 1, protowire.Fixed64Type)
	b = protowire.AppendFixed64(b, math.Float64bits(v))
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	return protowire.AppendVarint(b, uint64(ts))
}

type series struct {
	labels  [][2]string
	samples [][2]float64 // value, ms
}

func encRequest(ss []series, meta map[string]int32) []byte {
	var b []byte
	for _, s := range ss {
		var ts []byte
		for _, l := range s.labels {
			ts = protowire.AppendTag(ts, 1, protowire.BytesType)
			ts = protowire.AppendBytes(ts, encLabel(l[0], l[1]))
		}
		for _, smp := range s.samples {
			ts = protowire.AppendTag(ts, 2, protowire.BytesType)
			ts = protowire.AppendBytes(ts, encSample(smp[0], int64(smp[1])))
		}
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendBytes(b, ts)
	}
	for family, typ := range meta {
		var m []byte
		m = protowire.AppendTag(m, 1, protowire.VarintType)
		m = protowire.AppendVarint(m, uint64(typ))
		m = protowire.AppendTag(m, 2, protowire.BytesType)
		m = protowire.AppendString(m, family)
		b = protowire.AppendTag(b, 3, protowire.BytesType)
		b = protowire.AppendBytes(b, m)
	}
	return b
}

type captureIngestor struct {
	sig      domain.Signal
	payloads [][]byte
	err      error
}

func (c *captureIngestor) Ingest(_ context.Context, sig domain.Signal, p []byte) error {
	c.sig = sig
	c.payloads = append(c.payloads, p)
	return c.err
}

func post(h http.Handler, body []byte, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/write", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRemoteWrite(t *testing.T) {
	stale := math.Float64frombits(staleNaN)
	body := encRequest([]series{
		{labels: [][2]string{{"__name__", "http_requests_total"}, {"job", "api"}, {"instance", "10.0.0.1:9100"}, {"code", "200"}},
			samples: [][2]float64{{10, 1000}, {stale, 2000}, {12, 3000}}},
		{labels: [][2]string{{"__name__", "memory_bytes"}, {"job", "api"}, {"instance", "10.0.0.1:9100"}},
			samples: [][2]float64{{512, 1000}}},
		{labels: [][2]string{{"__name__", "latency_seconds_bucket"}, {"job", "db"}, {"le", "0.5"}},
			samples: [][2]float64{{7, 1000}}},
		{labels: [][2]string{{"job", "api"}}, samples: [][2]float64{{1, 1000}}}, // no name: dropped
	}, map[string]int32{"latency_seconds": metaHistogram, "memory_bytes": metaGauge})

	ing := &captureIngestor{}
	h := New(ing, nil, 1<<20, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if rec := post(h, snappy.Encode(nil, body)); rec.Code != http.StatusNoContent {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	if ing.sig != domain.SignalMetrics || len(ing.payloads) != 1 {
		t.Fatalf("ingest: %s, %d payloads", ing.sig, len(ing.payloads))
	}

	var req colmetrics.ExportMetricsServiceRequest
	if err := proto.Unmarshal(ing.payloads[0], &req); err != nil {
		t.Fatal(err)
	}
	if len(req.ResourceMetrics) != 2 {
		t.Fatalf("resources = %d, want 2 (api@instance, db)", len(req.ResourceMetrics))
	}
	api := req.ResourceMetrics[0]
	resAttrs := map[string]string{}
	for _, a := range api.Resource.Attributes {
		resAttrs[a.Key] = a.Value.GetStringValue()
	}
	if resAttrs["service.name"] != "api" || resAttrs["service.instance.id"] != "10.0.0.1:9100" {
		t.Errorf("resource = %v", resAttrs)
	}

	metrics := map[string]*metricspb.Metric{}
	for _, rm := range req.ResourceMetrics {
		for _, m := range rm.ScopeMetrics[0].Metrics {
			metrics[m.Name] = m
		}
	}
	reqs := metrics["http_requests_total"].GetSum()
	if reqs == nil || !reqs.IsMonotonic || len(reqs.DataPoints) != 2 {
		t.Fatalf("counter = %+v (stale marker must be dropped)", metrics["http_requests_total"])
	}
	if dp := reqs.DataPoints[1]; dp.TimeUnixNano != 3000*1e6 || dp.GetAsDouble() != 12 || dp.Attributes[0].Key != "code" {
		t.Errorf("data point = %+v", dp)
	}
	if metrics["memory_bytes"].GetGauge() == nil {
		t.Error("gauge metadata ignored")
	}
	if metrics["latency_seconds_bucket"].GetSum() == nil {
		t.Error("histogram bucket should be a cumulative sum")
	}
	if len(metrics) != 3 {
		t.Errorf("metrics = %d, unnamed series must be dropped", len(metrics))
	}
}

func TestRemoteWriteRejects(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ing := &captureIngestor{}
	h := New(ing, nil, 1<<20, log)
	good := snappy.Encode(nil, encRequest([]series{{labels: [][2]string{{"__name__", "up"}}, samples: [][2]float64{{1, 1}}}}, nil))

	cases := []struct {
		name    string
		body    []byte
		headers []string
		want    int
	}{
		{"not snappy", []byte("plain"), nil, http.StatusBadRequest},
		{"garbage protobuf", snappy.Encode(nil, []byte{0xff, 0xff, 0xff}), nil, http.StatusBadRequest},
		{"remote write 2.0", good, []string{"Content-Type", "application/x-protobuf;proto=io.prometheus.write.v2.Request"}, http.StatusUnsupportedMediaType},
		{"json", good, []string{"Content-Type", "application/json"}, http.StatusUnsupportedMediaType},
		{"gzip", good, []string{"Content-Encoding", "gzip"}, http.StatusUnsupportedMediaType},
		{"v1 explicit", good, []string{"Content-Type", "application/x-protobuf;proto=prometheus.WriteRequest"}, http.StatusNoContent},
	}
	for _, c := range cases {
		if rec := post(h, c.body, c.headers...); rec.Code != c.want {
			t.Errorf("%s: %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body)
		}
	}

	small := New(ing, nil, 16, log)
	if rec := post(small, good); rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Errorf("oversized: %d", rec.Code)
	}

	ing.err = domain.ErrUnavailable
	if rec := post(h, good); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unavailable should be retryable (5xx): %d", rec.Code)
	}
}

func TestIsCounterHeuristics(t *testing.T) {
	meta := map[string]int32{"rpc": metaSummary, "temp": metaGauge, "jobs": metaCounter}
	for name, want := range map[string]bool{
		"requests_total": true, "x_bucket": true, "x_count": true, "x_sum": true, "temperature": false,
		"rpc": false, "rpc_count": true, "temp": false, "jobs": true,
	} {
		if got := isCounter(name, meta); got != want {
			t.Errorf("isCounter(%q) = %v, want %v", name, got, want)
		}
	}
}
