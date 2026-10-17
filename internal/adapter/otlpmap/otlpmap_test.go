package otlpmap

import (
	"testing"
	"time"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

func str(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func resource(svc string) *resourcepb.Resource {
	return &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("service.name", svc), str("host.name", "h1")}}
}

func TestMapLogs(t *testing.T) {
	tid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	req := &collogs.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: resource("api"),
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{
			{TimeUnixNano: 1e18, SeverityText: "error", TraceId: tid,
				Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{
					Values: []*commonpb.KeyValue{str("msg", "boom")}}}}},
			{SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_WARN},
		}}},
	}}}
	now := time.Unix(42, 0).UTC()

	rows, res := MapLogs(req, now)

	if len(rows) != 2 || len(res) != 1 {
		t.Fatalf("got %d rows, %d resources", len(rows), len(res))
	}
	r := rows[0]
	if r.ServiceName != "api" || r.ResourceID != res[0].ID {
		t.Errorf("resource not linked: %+v", r)
	}
	if r.SeverityNumber != 17 || r.SeverityText != "error" {
		t.Errorf("severity from text: got %d %q", r.SeverityNumber, r.SeverityText)
	}
	if r.Body != `{"msg":"boom"}` {
		t.Errorf("body = %q", r.Body)
	}
	if r.TraceID != [16]byte(tid) {
		t.Errorf("trace id = %x", r.TraceID)
	}
	if rows[1].SeverityText != "WARN" || !rows[1].Timestamp.Equal(now) {
		t.Errorf("second row: %+v", rows[1])
	}
}

func TestResourceIDStableAcrossAttributeOrder(t *testing.T) {
	a := &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("a", "1"), str("b", "2")}}
	b := &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("b", "2"), str("a", "1")}}
	var r resources
	if r.add(a).ID != r.add(b).ID {
		t.Fatal("resource id depends on attribute order")
	}
	if len(r.list) != 1 {
		t.Fatalf("expected one distinct resource, got %d", len(r.list))
	}
}

func TestMapTraces(t *testing.T) {
	req := &coltrace.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: resource("api"),
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{
			TraceId: make([]byte, 16), SpanId: []byte{1, 2, 3, 4, 5, 6, 7, 8},
			Name: "GET /users", Kind: tracepb.Span_SPAN_KIND_SERVER,
			StartTimeUnixNano: 1000, EndTimeUnixNano: 4000,
			Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "x"},
			Events: []*tracepb.Span_Event{{TimeUnixNano: 2000, Name: "exception"}},
		}}}},
	}}}

	rows, _ := MapTraces(req)

	s := rows[0]
	if s.DurationNs != 3000 || s.Kind != domain.KindServer || s.StatusCode != domain.StatusError {
		t.Errorf("unexpected span: %+v", s)
	}
	if !s.IsEntry() || !s.IsError() {
		t.Error("root server span with error status should be entry and error")
	}
	if len(s.EventNames) != 1 || s.EventNames[0] != "exception" {
		t.Errorf("events = %v", s.EventNames)
	}
}

func TestMapMetricsHistogramIsCumulative(t *testing.T) {
	sum := 12.0
	req := &colmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: resource("api"),
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{
			{Name: "latency", Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
				AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
				DataPoints: []*metricspb.HistogramDataPoint{{
					TimeUnixNano: 1e9, Count: 6, Sum: &sum,
					ExplicitBounds: []float64{0.1, 1}, BucketCounts: []uint64{1, 2, 3},
				}},
			}}},
			{Name: "up", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
				DataPoints: []*metricspb.NumberDataPoint{{Value: &metricspb.NumberDataPoint_AsInt{AsInt: 1}}},
			}}},
			{Name: "exp", Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
				DataPoints: []*metricspb.ExponentialHistogramDataPoint{{}},
			}}},
		}}},
	}}}

	m := MapMetrics(req)

	byName := map[string][]float64{}
	names := map[uint64]string{}
	for _, s := range m.Series {
		names[s.ID] = s.MetricName + "{le=" + s.Attributes["le"] + "}"
	}
	for _, smp := range m.Samples {
		byName[names[smp.SeriesID]] = append(byName[names[smp.SeriesID]], smp.Value)
	}
	want := map[string]float64{
		"latency_bucket{le=0.1}":  1,
		"latency_bucket{le=1}":    3,
		"latency_bucket{le=+Inf}": 6,
		"latency_sum{le=}":        12,
		"latency_count{le=}":      6,
		"up{le=}":                 1,
	}
	for k, v := range want {
		if got := byName[k]; len(got) != 1 || got[0] != v {
			t.Errorf("%s = %v, want %v", k, got, v)
		}
	}
	if m.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", m.Skipped)
	}
}

func TestDecode(t *testing.T) {
	payload, _ := proto.Marshal(&collogs.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{}}}},
	}}})
	b, _, err := Decode(domain.SignalLogs, payload, time.Now())
	if err != nil || len(b.Logs) != 1 || b.Logs[0].ServiceName != unknownService {
		t.Fatalf("decode: %v %+v", err, b)
	}
	if _, _, err := Decode(domain.SignalLogs, []byte{0xff, 0xff}, time.Now()); err == nil {
		t.Error("expected error for garbage payload")
	}
	if _, _, err := Decode("nope", payload, time.Now()); err == nil {
		t.Error("expected error for unknown signal")
	}
}
