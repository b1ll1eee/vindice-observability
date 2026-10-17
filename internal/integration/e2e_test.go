//go:build integration

// Package integration runs the whole write path against a real ClickHouse
// and an in-process Kafka (kfake):
//
//	OTLP/HTTP -> IngestService -> Kafka -> consumer -> WriterService -> ClickHouse
//
// Run with: VINDICE_IT_CH_ADDR=localhost:9000 go test -tags integration ./internal/integration/
package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/twmb/franz-go/pkg/kfake"
	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/kafkaconsumer"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/otlphttp"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/kafka"
	chrepo "github.com/b1ll1eee/vindice-obs/internal/adapter/out/clickhouse"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/out/kafkapublisher"
	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/policy"
	"github.com/b1ll1eee/vindice-obs/internal/core/service"
)

func TestEndToEnd(t *testing.T) {
	chAddr := os.Getenv("VINDICE_IT_CH_ADDR")
	if chAddr == "" {
		t.Skip("VINDICE_IT_CH_ADDR not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Kafka
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1))
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	brokers := cluster.ListenAddrs()
	if err := kafka.EnsureTopics(ctx, brokers, 3, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := kafka.EnsureTopics(ctx, brokers, 3, time.Hour); err != nil {
		t.Fatalf("EnsureTopics must be idempotent: %v", err)
	}

	// ClickHouse, in a throwaway database
	db := fmt.Sprintf("vindice_it_%d", time.Now().UnixNano())
	cfg := chrepo.Config{Addrs: []string{chAddr}, Database: db, User: "default", Retention: chrepo.Retention{
		DebugLogs: 3, InfoLogs: 14, ErrorLogs: 90, Traces: 7, MetricsRaw: 7, Metrics1m: 30, Metrics1h: 365,
	}}
	repo, err := chrepo.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for range 2 { // must be idempotent
		if err := repo.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := clickhouse.Open(&clickhouse.Options{Addr: []string{chAddr}, Auth: clickhouse.Auth{Database: db}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db)
		_ = conn.Close()
	}()

	// Writer side: drop healthz spans, keep only error/slow traces (rate 0).
	filter, err := policy.NewFilter([]policy.Rule{{Signal: domain.SignalTraces, Name: "healthz"}})
	if err != nil {
		t.Fatal(err)
	}
	writer := service.NewWriterService(service.WriterDeps{
		Repo:      repo,
		Filter:    filter,
		Sampler:   policy.NewSampler(policy.SamplerConfig{Rate: 0, Slow: time.Second, Wait: 300 * time.Millisecond}),
		Resources: policy.NewSeen(time.Hour, 1000),
		Series:    policy.NewSeen(time.Hour, 1000),
	})
	consumer, err := kafkaconsumer.New(kafkaconsumer.Config{
		Brokers: brokers, Group: "it", BatchRows: 10_000, FlushInterval: 200 * time.Millisecond,
	}, writer, log)
	if err != nil {
		t.Fatal(err)
	}
	consumerCtx, stopConsumer := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- consumer.Run(consumerCtx) }()

	// Ingest side, through the real HTTP adapter.
	pub, err := kafkapublisher.New(brokers)
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close()
	srv := httptest.NewServer(otlphttp.New(service.NewIngestService(pub), 16<<20, log))
	defer srv.Close()

	post(t, srv.URL+"/v1/traces", traces())
	post(t, srv.URL+"/v1/logs", logs())
	post(t, srv.URL+"/v1/metrics", metrics())

	count := func(q string) uint64 {
		var n uint64
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	waitFor(t, func() bool { return count("SELECT count() FROM logs") == 3 && count("SELECT count() FROM spans") == 2 })

	stopConsumer()
	if err := <-done; err != nil {
		t.Fatalf("consumer: %v", err)
	}

	checks := []struct {
		name  string
		query string
		want  uint64
	}{
		{"resources deduplicated", "SELECT count() FROM resources FINAL", 1},
		{"logs", "SELECT count() FROM logs", 3},
		{"log severity derived from text", "SELECT count() FROM logs WHERE severity_number = 17 AND severity_text = 'ERROR'", 1},
		{"log trace id stored as binary", "SELECT count() FROM logs WHERE trace_id = unhex('0102030405060708090a0b0c0d0e0f10')", 1},
		{"log full-text search", "SELECT count() FROM logs WHERE hasToken(lower(body), 'timeout')", 1},
		{"log counts rollup", "SELECT sum(records) FROM log_counts_1m", 3},
		{"only the error trace is sampled", "SELECT count() FROM spans", 2},
		{"healthz dropped before RED", "SELECT count() FROM spans WHERE span_name = 'GET /healthz'", 0},
		{"RED counts every entry span before sampling", "SELECT sum(requests) FROM red_1m", 2},
		{"RED errors", "SELECT sum(errors) FROM red_1m", 1},
		{"RED p95 readable", "SELECT count() FROM (SELECT quantilesTDigestMerge(0.5, 0.95, 0.99)(duration_q) AS q FROM red_1m GROUP BY service_name HAVING q[2] > 0)", 1},
		{"http attributes promoted", "SELECT count() FROM spans WHERE http_route = '/orders' AND http_status_code = 500 AND NOT mapContains(attributes, 'http.route')", 1},
		{"trace index", "SELECT count() FROM trace_index WHERE trace_id = unhex('0102030405060708090a0b0c0d0e0f10')", 1},
		{"metric series", "SELECT count() FROM metric_series FINAL", 6},
		{"metric samples", "SELECT count() FROM metric_samples", 6},
		{"metric 1m rollup", "SELECT count() FROM metric_samples_1m", 6},
		{"metric 1h rollup", "SELECT count() FROM metric_samples_1h", 6},
		{"histogram buckets cumulative", "SELECT toUInt64(max(value)) FROM metric_samples s JOIN metric_series m USING series_id WHERE m.metric_name = 'latency_bucket'", 6},
	}
	for _, c := range checks {
		if got := count(c.query); got != c.want {
			t.Errorf("%s: got %d, want %d\n  %s", c.name, got, c.want, c.query)
		}
	}

	var ttl string
	if err := conn.QueryRow(ctx, "SELECT create_table_query FROM system.tables WHERE database = ? AND name = 'logs'", db).Scan(&ttl); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(ttl), []byte("WHERE (severity_number >= 1) AND (severity_number <= 8)")) &&
		!bytes.Contains([]byte(ttl), []byte("severity_number BETWEEN 1 AND 8")) {
		t.Errorf("severity-based TTL missing:\n%s", ttl)
	}
}

func post(t *testing.T, url string, m proto.Message) {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/x-protobuf", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: %d %s", url, resp.StatusCode, body)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for rows")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func kv(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

var (
	res        = &resourcepb.Resource{Attributes: []*commonpb.KeyValue{kv("service.name", "orders"), kv("host.name", "node-1")}}
	errTraceID = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	okTraceID  = []byte{9, 9, 9, 9, 9, 9, 9, 9, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
)

func traces() *coltrace.ExportTraceServiceRequest {
	now := uint64(time.Now().UnixNano())
	return &coltrace.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: res,
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{
			{ // failing request: root + child, kept by the sampler
				TraceId: errTraceID, SpanId: []byte{1, 0, 0, 0, 0, 0, 0, 1}, Name: "POST /orders",
				Kind: tracepb.Span_SPAN_KIND_SERVER, StartTimeUnixNano: now, EndTimeUnixNano: now + 5e6,
				Status:     &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR},
				Attributes: []*commonpb.KeyValue{kv("http.route", "/orders"), kv("http.response.status_code", "500")},
			},
			{
				TraceId: errTraceID, SpanId: []byte{1, 0, 0, 0, 0, 0, 0, 2}, ParentSpanId: []byte{1, 0, 0, 0, 0, 0, 0, 1},
				Name: "SELECT orders", Kind: tracepb.Span_SPAN_KIND_CLIENT, StartTimeUnixNano: now + 1e6, EndTimeUnixNano: now + 4e6,
			},
			{ // healthy request: counted in RED, dropped by sampling (rate 0)
				TraceId: okTraceID, SpanId: []byte{2, 0, 0, 0, 0, 0, 0, 1}, Name: "GET /orders",
				Kind: tracepb.Span_SPAN_KIND_SERVER, StartTimeUnixNano: now, EndTimeUnixNano: now + 2e6,
				Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK},
			},
			{ // health check: dropped by rule before anything else
				TraceId: okTraceID, SpanId: []byte{3, 0, 0, 0, 0, 0, 0, 1}, Name: "GET /healthz",
				Kind: tracepb.Span_SPAN_KIND_SERVER, StartTimeUnixNano: now, EndTimeUnixNano: now + 1e5,
			},
		}}},
	}}}
}

func logs() *collogs.ExportLogsServiceRequest {
	now := uint64(time.Now().UnixNano())
	body := func(s string) *commonpb.AnyValue {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
	}
	return &collogs.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: res,
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{
			{TimeUnixNano: now, SeverityText: "ERROR", Body: body("db Timeout after 3s"), TraceId: errTraceID},
			{TimeUnixNano: now, SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_INFO, Body: body("order created")},
			{TimeUnixNano: now, Body: body("no severity")},
		}}},
	}}}
}

func metrics() *colmetrics.ExportMetricsServiceRequest {
	now := uint64(time.Now().UnixNano())
	sum := 3.5
	return &colmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: res,
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{
			{Name: "up", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{
				{TimeUnixNano: now, Value: &metricspb.NumberDataPoint_AsInt{AsInt: 1}},
			}}}},
			{Name: "latency", Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
				AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
				DataPoints: []*metricspb.HistogramDataPoint{{
					TimeUnixNano: now, Count: 6, Sum: &sum, ExplicitBounds: []float64{0.1, 1}, BucketCounts: []uint64{1, 2, 3},
				}},
			}}},
		}}},
	}}}
}
