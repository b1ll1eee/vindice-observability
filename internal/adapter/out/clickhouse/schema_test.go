package clickhouse

import (
	"strings"
	"testing"
)

func TestRenderSchema(t *testing.T) {
	cfg := Config{Retention: Retention{
		DebugLogs: 3, InfoLogs: 14, ErrorLogs: 90, Traces: 7, MetricsRaw: 7, Metrics1m: 30, Metrics1h: 365,
	}}
	stmts, err := renderSchema(cfg.ttls())
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 20 {
		t.Errorf("got %d statements, want 20", len(stmts))
	}
	all := strings.Join(stmts, "\n")
	for _, want := range []string{
		"INTERVAL 3 DAY DELETE WHERE severity_number BETWEEN 1 AND 8",
		"INTERVAL 90 DAY",
		"last_seen + INTERVAL 365 DAY",
		"ts + INTERVAL 365 DAY",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("schema missing %q", want)
		}
	}
	if strings.Contains(all, "{{") || strings.Contains(all, "storage_policy") {
		t.Error("unrendered template or unexpected storage policy")
	}
}

func TestColdTier(t *testing.T) {
	cfg := Config{ColdStoragePolicy: "tiered", ColdAfterDays: 5, Retention: Retention{Traces: 7}}
	ttl := cfg.ttls()
	if !strings.HasPrefix(ttl.SpansTTL, "toDateTime(timestamp) + INTERVAL 5 DAY TO VOLUME 'cold'") {
		t.Errorf("spans ttl = %s", ttl.SpansTTL)
	}
	if ttl.ColdSettings != "SETTINGS storage_policy = 'tiered'" {
		t.Errorf("settings = %s", ttl.ColdSettings)
	}
	if strings.Contains(ttl.Rollup1mTTL, "cold") {
		t.Error("rollups are small and should stay on local disk")
	}
}
