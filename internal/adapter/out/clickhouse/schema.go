package clickhouse

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"text/template"

	"github.com/ClickHouse/clickhouse-go/v2"
)

//go:embed schema/*.sql
var schemaFS embed.FS

// ttls holds the TTL clause of every table, derived from the retention config.
type ttls struct {
	ColdSettings  string
	DimensionTTL  string
	LogsTTL       string
	SpansTTL      string
	TraceIndexTTL string
	SamplesTTL    string
	Rollup1mTTL   string
	Rollup1hTTL   string
}

func (c Config) ttls() ttls {
	r := c.Retention
	cold, settings := "", ""
	if c.ColdStoragePolicy != "" {
		cold = fmt.Sprintf("toDateTime(timestamp) + INTERVAL %d DAY TO VOLUME 'cold', ", c.ColdAfterDays)
		settings = fmt.Sprintf("SETTINGS storage_policy = '%s'", c.ColdStoragePolicy)
	}
	return ttls{
		ColdSettings: settings,
		DimensionTTL: fmt.Sprintf("last_seen + INTERVAL %d DAY", r.MaxDays()),
		// Severity 0 (unknown) is kept like INFO.
		LogsTTL: cold + fmt.Sprintf(
			"toDateTime(timestamp) + INTERVAL %d DAY DELETE WHERE severity_number BETWEEN 1 AND 8, "+
				"toDateTime(timestamp) + INTERVAL %d DAY DELETE WHERE severity_number < 17, "+
				"toDateTime(timestamp) + INTERVAL %d DAY",
			r.DebugLogs, r.InfoLogs, r.ErrorLogs),
		SpansTTL:      cold + fmt.Sprintf("toDateTime(timestamp) + INTERVAL %d DAY", r.Traces),
		TraceIndexTTL: fmt.Sprintf("toDateTime(end_ts) + INTERVAL %d DAY", r.Traces),
		SamplesTTL:    cold + fmt.Sprintf("toDateTime(timestamp) + INTERVAL %d DAY", r.MetricsRaw),
		Rollup1mTTL:   fmt.Sprintf("ts + INTERVAL %d DAY", r.Metrics1m),
		Rollup1hTTL:   fmt.Sprintf("ts + INTERVAL %d DAY", r.Metrics1h),
	}
}

// Migrate creates missing tables and views, then re-applies every TTL so that
// retention changes in config take effect on existing tables. TTLs are not
// materialized on old parts (that would rewrite all data); old parts pick up
// the new TTL as they merge.
func (r *Repository) Migrate(ctx context.Context) error {
	t := r.cfg.ttls()
	stmts, err := renderSchema(t)
	if err != nil {
		return err
	}
	for _, s := range stmts {
		if err := r.conn.Exec(ctx, s); err != nil {
			return fmt.Errorf("migrate: %w\n%s", err, s)
		}
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"materialize_ttl_after_modify": 0}))
	for table, ttl := range map[string]string{
		"resources":         t.DimensionTTL,
		"metric_series":     t.DimensionTTL,
		"logs":              t.LogsTTL,
		"log_counts_1m":     t.Rollup1mTTL,
		"spans":             t.SpansTTL,
		"trace_index":       t.TraceIndexTTL,
		"red_1m":            t.Rollup1mTTL,
		"metric_samples":    t.SamplesTTL,
		"metric_samples_1m": t.Rollup1mTTL,
		"metric_samples_1h": t.Rollup1hTTL,
	} {
		if err := r.conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s MODIFY TTL %s", table, ttl)); err != nil {
			return fmt.Errorf("migrate: ttl %s: %w", table, err)
		}
	}
	return nil
}

func renderSchema(t ttls) ([]string, error) {
	files, err := fs.Glob(schemaFS, "schema/*.sql")
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	var stmts []string
	for _, f := range files {
		tmpl, err := template.ParseFS(schemaFS, f)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, t); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, s := range strings.Split(buf.String(), ";\n") {
			if s = strings.TrimSpace(s); s != "" {
				stmts = append(stmts, s)
			}
		}
	}
	return stmts, nil
}
