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
	"strings"
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

	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/httpapi"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/kafkaconsumer"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/otlphttp"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/kafka"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/out/authcrypto"
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
	user, password := os.Getenv("VINDICE_IT_CH_USER"), os.Getenv("VINDICE_IT_CH_PASSWORD")
	if user == "" {
		user = "default"
	}
	cfg := chrepo.Config{Addrs: []string{chAddr}, Database: db, User: user, Password: password, Retention: chrepo.Retention{
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
	conn, err := clickhouse.Open(&clickhouse.Options{Addr: []string{chAddr}, Auth: clickhouse.Auth{Database: db, Username: user, Password: password}})
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

	testReadSide(ctx, t, service.NewQueryService(repo))
	testMetricsDashboardsAlerts(ctx, t, repo)
	testAuth(ctx, t, repo)
}

func testAuth(ctx context.Context, t *testing.T, repo *chrepo.Repository) {
	key1, err := repo.GetOrCreateSecret(ctx, "session-key", func() string { return strings.Repeat("a", 64) })
	key2, _ := repo.GetOrCreateSecret(ctx, "session-key", func() string { return strings.Repeat("b", 64) })
	if err != nil || key1 != key2 || key1 != strings.Repeat("a", 64) {
		t.Fatalf("secret not stable: %q %q %v", key1, key2, err)
	}
	codec, _ := authcrypto.NewHMAC([]byte(key1))
	auth, err := service.NewAuthService(repo, authcrypto.Bcrypt{Cost: 4}, codec, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if gen, err := auth.Bootstrap(ctx, "admin", "admin-password-1"); err != nil || gen != "" {
		t.Fatalf("bootstrap: %q %v", gen, err)
	}
	viewer, err := auth.CreateUser(ctx, "viewer1", domain.RoleViewer, "viewer-password")
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(httpapi.New(httpapi.Deps{
		Querier: service.NewQueryService(repo), Dashboards: service.NewDashboardService(repo),
		Auth: auth, Users: auth, SessionTTL: time.Hour, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	defer srv.Close()

	do := func(method, path, cookie, body string) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "vindice_session", Value: cookie})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	login := func(user, pw string) (string, int) {
		resp := do("POST", "/api/v1/auth/login", "", fmt.Sprintf(`{"username":%q,"password":%q}`, user, pw))
		for _, c := range resp.Cookies() {
			if c.Name == "vindice_session" {
				return c.Value, resp.StatusCode
			}
		}
		return "", resp.StatusCode
	}

	if _, code := login("admin", "wrong-password"); code != http.StatusUnauthorized {
		t.Errorf("bad password: %d", code)
	}
	if code := do("GET", "/api/v1/services", "", "").StatusCode; code != http.StatusUnauthorized {
		t.Errorf("anonymous read: %d", code)
	}
	adminCookie, code := login("admin", "admin-password-1")
	if code != http.StatusOK || adminCookie == "" {
		t.Fatalf("admin login: %d", code)
	}
	viewerCookie, _ := login("viewer1", "viewer-password")

	if code := do("GET", "/api/v1/services", viewerCookie, "").StatusCode; code != http.StatusOK {
		t.Errorf("viewer read: %d", code)
	}
	if code := do("POST", "/api/v1/dashboards", viewerCookie, `{"title":"x","panels":[]}`).StatusCode; code != http.StatusForbidden {
		t.Errorf("viewer write: %d", code)
	}
	if code := do("POST", "/api/v1/dashboards", adminCookie, `{"title":"x","panels":[]}`).StatusCode; code != http.StatusOK {
		t.Errorf("admin write: %d", code)
	}
	if code := do("GET", "/api/v1/users", viewerCookie, "").StatusCode; code != http.StatusForbidden {
		t.Errorf("viewer lists users: %d", code)
	}

	// Disabling the viewer ends their session at once; logout ends the admin's.
	if code := do("PUT", "/api/v1/users/"+viewer.ID, adminCookie, `{"role":"viewer","disabled":true}`).StatusCode; code != http.StatusOK {
		t.Fatalf("disable viewer: %d", code)
	}
	if code := do("GET", "/api/v1/services", viewerCookie, "").StatusCode; code != http.StatusUnauthorized {
		t.Errorf("disabled viewer still in: %d", code)
	}
	if code := do("POST", "/api/v1/auth/logout", adminCookie, "").StatusCode; code != http.StatusNoContent {
		t.Fatalf("logout: %d", code)
	}
	if code := do("GET", "/api/v1/auth/me", adminCookie, "").StatusCode; code != http.StatusUnauthorized {
		t.Errorf("cookie still valid after logout: %d", code)
	}
}

type captureNotifier struct{ sent []domain.Notification }

func (c *captureNotifier) Notify(_ context.Context, n domain.Notification) error {
	c.sent = append(c.sent, n)
	return nil
}

func testMetricsDashboardsAlerts(ctx context.Context, t *testing.T, repo *chrepo.Repository) {
	q := service.NewQueryService(repo)
	window := domain.TimeRange{From: time.Now().Add(-10 * time.Minute), To: time.Now().Add(time.Minute)}

	names, err := q.MetricNames(ctx, "orders", window)
	if err != nil || len(names) != 4 { // up, latency_bucket, latency_sum, latency_count
		t.Errorf("metric names = %+v, %v", names, err)
	}

	for _, c := range []struct {
		q    domain.MetricQuery
		want float64
	}{
		{domain.MetricQuery{Name: "up", Agg: domain.AggAvg}, 1},
		{domain.MetricQuery{Name: "latency_count", Agg: domain.AggLast}, 6},
		{domain.MetricQuery{Name: "latency_bucket", Agg: domain.AggMax, Filters: map[string]string{"le": "1"}}, 3},
	} {
		res, err := q.QueryMetric(ctx, c.q, window)
		if err != nil || len(res.Series) != 1 {
			t.Errorf("metric %s: %+v, %v", c.q.Name, res, err)
			continue
		}
		var got []float64
		for _, p := range res.Series[0].Points {
			if p.Valid {
				got = append(got, p.Value)
			}
		}
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("metric %s %s: points %v, want one %v", c.q.Agg, c.q.Name, got, c.want)
		}
	}
	grouped, err := q.QueryMetric(ctx, domain.MetricQuery{Name: "latency_bucket", Agg: domain.AggLast, GroupBy: "le"}, window)
	if err != nil || len(grouped.Series) != 3 {
		t.Errorf("group by le: %d series, %v", len(grouped.Series), err)
	}

	_, red, err := q.ServiceSeries(ctx, "orders", window)
	var reqs, errs uint64
	for _, p := range red {
		reqs += p.Requests
		errs += p.Errors
	}
	if err != nil || reqs != 2 || errs != 1 {
		t.Errorf("service series: %d requests, %d errors, %v", reqs, errs, err)
	}

	// Dashboards: create, update, delete.
	dash := service.NewDashboardService(repo)
	panel := domain.Panel{Title: "errors", Width: 6, Signal: domain.SignalQuery{
		Kind: domain.SignalKindRED, Service: "orders", Measure: domain.MeasureErrorRate}}
	d, err := dash.SaveDashboard(ctx, domain.Dashboard{Title: "Orders", Panels: []domain.Panel{panel}})
	if err != nil || d.ID == "" || d.Panels[0].ID == "" {
		t.Fatalf("create dashboard: %+v, %v", d, err)
	}
	d.Title = "Orders v2"
	time.Sleep(time.Millisecond) // newer version
	if _, err := dash.SaveDashboard(ctx, d); err != nil {
		t.Fatal(err)
	}
	if got, err := dash.GetDashboard(ctx, d.ID); err != nil || got.Title != "Orders v2" || len(got.Panels) != 1 {
		t.Errorf("get dashboard: %+v, %v", got, err)
	}
	if err := dash.DeleteDashboard(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := dash.ListDashboards(ctx); len(list) != 0 {
		t.Errorf("deleted dashboard still listed: %+v", list)
	}
	if _, err := dash.GetDashboard(ctx, d.ID); err != domain.ErrNotFound {
		t.Errorf("deleted dashboard: %v", err)
	}

	// Alerts: one firing rule per signal kind, evaluated against real data.
	notif := &captureNotifier{}
	alerts := service.NewAlertService(service.AlertDeps{
		Rules: repo, State: repo, Signals: q, Notifier: notif, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	rules := []domain.AlertRule{
		{Name: "error rate", Signal: domain.SignalQuery{Kind: domain.SignalKindRED, Service: "orders", Measure: domain.MeasureErrorRate},
			Op: domain.OpGE, Threshold: 0.5},
		{Name: "error logs", Signal: domain.SignalQuery{Kind: domain.SignalKindLogs, Service: "orders", MinSeverity: 17},
			Op: domain.OpGE, Threshold: 1},
		{Name: "down", Signal: domain.SignalQuery{Kind: domain.SignalKindMetric, Metric: &domain.MetricQuery{Name: "up", Agg: domain.AggMin}},
			Op: domain.OpLT, Threshold: 2},
		{Name: "quiet", Signal: domain.SignalQuery{Kind: domain.SignalKindRED, Service: "orders", Measure: domain.MeasureRequests},
			Op: domain.OpGT, Threshold: 1000},
	}
	for _, r := range rules {
		r.Enabled, r.Window = true, 10*time.Minute
		if _, err := alerts.SaveRule(ctx, r); err != nil {
			t.Fatalf("save rule %s: %v", r.Name, err)
		}
	}
	if err := alerts.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	fired := map[string]float64{}
	for _, n := range notif.sent {
		fired[n.Rule.Name] = n.Value
	}
	if len(fired) != 3 || fired["error rate"] != 0.5 || fired["error logs"] != 1 || fired["down"] != 1 {
		t.Errorf("fired = %v", fired)
	}

	statuses, err := repo.LoadStatuses(ctx)
	if err != nil || len(statuses) != 4 {
		t.Errorf("persisted statuses: %d, %v", len(statuses), err)
	}
	events, err := alerts.Events(ctx, "", 10)
	if err != nil || len(events) != 3 || events[0].State != domain.EventFiring {
		t.Errorf("events = %+v, %v", events, err)
	}

	// A second round must not notify again.
	if err := alerts.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if len(notif.sent) != 3 {
		t.Errorf("re-notified: %d notifications", len(notif.sent))
	}
}

// testReadSide runs every query through the real ClickHouse reader.
func testReadSide(ctx context.Context, t *testing.T, q *service.QueryService) {
	window := domain.TimeRange{From: time.Now().Add(-10 * time.Minute), To: time.Now().Add(time.Minute)}

	svcs, err := q.Services(ctx, window)
	if err != nil {
		t.Fatalf("services: %v", err)
	}
	if len(svcs) != 1 || svcs[0].Name != "orders" || svcs[0].Requests != 2 || svcs[0].Errors != 1 ||
		svcs[0].Logs != 3 || svcs[0].LogErrors != 1 || svcs[0].Latency.P95 <= 0 {
		t.Errorf("services = %+v", svcs)
	}

	ops, err := q.Operations(ctx, "orders", window)
	if err != nil || len(ops) != 2 {
		t.Errorf("operations = %+v, %v", ops, err)
	}

	for name, c := range map[string]struct {
		query domain.LogQuery
		want  int
	}{
		"all":                {domain.LogQuery{Range: window}, 3},
		"text, any case":     {domain.LogQuery{Range: window, Text: "TIMEOUT"}, 1},
		"phrase":             {domain.LogQuery{Range: window, Text: "order created"}, 1},
		"no match":           {domain.LogQuery{Range: window, Text: "nothing-like-this"}, 0},
		"errors only":        {domain.LogQuery{Range: window, MinSeverity: 17}, 1},
		"service":            {domain.LogQuery{Range: window, Service: "orders"}, 3},
		"trace":              {domain.LogQuery{Range: window, TraceID: (*[16]byte)(errTraceID)}, 1},
		"page size":          {domain.LogQuery{Range: window, Limit: 2}, 2},
		"cursor before data": {domain.LogQuery{Range: window, Before: time.Now().Add(-5 * time.Minute)}, 0},
	} {
		page, err := q.SearchLogs(ctx, c.query)
		if err != nil {
			t.Errorf("logs %s: %v", name, err)
			continue
		}
		if len(page.Entries) != c.want {
			t.Errorf("logs %s: got %d, want %d", name, len(page.Entries), c.want)
		}
		if len(page.Entries) > 0 && page.Entries[0].Resource["host.name"] != "node-1" {
			t.Errorf("logs %s: resource not attached: %v", name, page.Entries[0].Resource)
		}
	}

	for name, lq := range map[string]domain.LogQuery{
		"rollup": {Range: window},
		"raw":    {Range: window, Text: "timeout"},
	} {
		h, err := q.LogHistogram(ctx, lq)
		if err != nil {
			t.Errorf("histogram %s: %v", name, err)
			continue
		}
		var total, errs uint64
		for _, b := range h.Buckets {
			total += b.Debug + b.Info + b.Warn + b.Errors
			errs += b.Errors
		}
		if name == "rollup" && (total != 3 || errs != 1) {
			t.Errorf("histogram rollup: total %d errors %d", total, errs)
		}
		if name == "raw" && (total != 1 || errs != 1) {
			t.Errorf("histogram raw: total %d errors %d", total, errs)
		}
	}

	traces, err := q.SearchTraces(ctx, domain.TraceQuery{Range: window, ErrorsOnly: true})
	if err != nil || len(traces) != 1 || traces[0].Name != "POST /orders" || traces[0].HTTPStatus != 500 {
		t.Errorf("traces = %+v, %v", traces, err)
	}
	if slow, _ := q.SearchTraces(ctx, domain.TraceQuery{Range: window, MinDuration: time.Hour}); len(slow) != 0 {
		t.Errorf("min duration filter: %d traces", len(slow))
	}

	tr, err := q.Trace(ctx, [16]byte(errTraceID))
	if err != nil {
		t.Fatalf("trace: %v", err)
	}
	if len(tr.Spans) != 2 || len(tr.Logs) != 1 || tr.Resources[tr.Spans[0].ResourceID]["host.name"] != "node-1" {
		t.Errorf("trace = %d spans, %d logs, resources %v", len(tr.Spans), len(tr.Logs), tr.Resources)
	}
	if tr.Spans[1].ParentSpanID != tr.Spans[0].SpanID {
		t.Errorf("child span not linked to root")
	}
	if _, err := q.Trace(ctx, [16]byte{0xde, 0xad}); err != domain.ErrNotFound {
		t.Errorf("unknown trace: %v", err)
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
