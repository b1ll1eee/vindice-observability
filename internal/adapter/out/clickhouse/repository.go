// Package clickhouse implements the telemetry repositories and schema
// migration on ClickHouse.
package clickhouse

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

type Retention struct {
	DebugLogs, InfoLogs, ErrorLogs   int
	Traces                           int
	MetricsRaw, Metrics1m, Metrics1h int
}

func (r Retention) MaxDays() int {
	return max(r.DebugLogs, r.InfoLogs, r.ErrorLogs, r.Traces, r.MetricsRaw, r.Metrics1m, r.Metrics1h)
}

type Config struct {
	Addrs    []string
	Database string
	User     string
	Password string

	Retention Retention
	// ColdStoragePolicy, when set, must name a server storage policy with a
	// volume called 'cold' (e.g. S3 or R2); old parts move there after ColdAfterDays.
	ColdStoragePolicy string
	ColdAfterDays     int
}

type Repository struct {
	conn driver.Conn
	cfg  Config
}

var (
	_ port.TelemetryRepository = (*Repository)(nil)
	_ port.SchemaMigrator      = (*Repository)(nil)
)

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Open connects, creating the database if needed.
func Open(ctx context.Context, cfg Config) (*Repository, error) {
	if !identRe.MatchString(cfg.Database) {
		return nil, fmt.Errorf("invalid database name %q", cfg.Database)
	}
	if cfg.ColdStoragePolicy != "" && !identRe.MatchString(cfg.ColdStoragePolicy) {
		return nil, fmt.Errorf("invalid storage policy name %q", cfg.ColdStoragePolicy)
	}

	boot, err := connect(ctx, cfg, "default")
	if err != nil {
		return nil, err
	}
	err = boot.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+cfg.Database)
	_ = boot.Close()
	if err != nil {
		return nil, err
	}

	conn, err := connect(ctx, cfg, cfg.Database)
	if err != nil {
		return nil, err
	}
	return &Repository{conn: conn, cfg: cfg}, nil
}

func connect(ctx context.Context, cfg Config, db string) (driver.Conn, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr:        cfg.Addrs,
		Auth:        clickhouse.Auth{Database: db, Username: cfg.User, Password: cfg.Password},
		Compression: &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (r *Repository) Close() error { return r.conn.Close() }

func (r *Repository) SaveResources(ctx context.Context, rows []domain.Resource) error {
	now := time.Now()
	return insert(ctx, r.conn,
		"INSERT INTO resources (resource_id, service_name, attributes, last_seen)",
		rows, func(x *domain.Resource) []any {
			return []any{x.ID, x.ServiceName, x.Attributes, now}
		})
}

func (r *Repository) SaveLogs(ctx context.Context, rows []domain.LogRow) error {
	return insert(ctx, r.conn,
		"INSERT INTO logs (timestamp, service_name, severity_number, severity_text, trace_id, span_id, "+
			"resource_id, scope_name, body, attributes)",
		rows, func(x *domain.LogRow) []any {
			return []any{x.Timestamp, x.ServiceName, x.SeverityNumber, x.SeverityText, string(x.TraceID[:]),
				string(x.SpanID[:]), x.ResourceID, x.ScopeName, x.Body, x.Attributes}
		})
}

func (r *Repository) SaveSpans(ctx context.Context, rows []domain.SpanRow) error {
	return insert(ctx, r.conn,
		"INSERT INTO spans (timestamp, trace_id, span_id, parent_span_id, service_name, span_name, span_kind, "+
			"status_code, status_message, duration_ns, resource_id, scope_name, http_method, http_route, "+
			"http_status_code, attributes, events_timestamp, events_name, events_attributes)",
		rows, func(x *domain.SpanRow) []any {
			return []any{x.Timestamp, string(x.TraceID[:]), string(x.SpanID[:]), string(x.ParentSpanID[:]),
				x.ServiceName, x.Name, x.Kind, x.StatusCode, x.StatusMessage, x.DurationNs, x.ResourceID,
				x.ScopeName, x.HTTPMethod, x.HTTPRoute, x.HTTPStatus, x.Attributes, x.EventTimes, x.EventNames,
				x.EventAttrs}
		})
}

func (r *Repository) SaveSpanStats(ctx context.Context, rows []domain.SpanStat) error {
	return insert(ctx, r.conn,
		"INSERT INTO span_stats (timestamp, service_name, span_name, status_code, duration_ns)",
		rows, func(x *domain.SpanStat) []any {
			return []any{x.Timestamp, x.ServiceName, x.Name, x.StatusCode, x.DurationNs}
		})
}

func (r *Repository) SaveSeries(ctx context.Context, rows []domain.Series) error {
	now := time.Now()
	return insert(ctx, r.conn,
		"INSERT INTO metric_series (series_id, metric_name, service_name, metric_type, temporality, "+
			"is_monotonic, unit, resource_id, attributes, last_seen)",
		rows, func(x *domain.Series) []any {
			return []any{x.ID, x.MetricName, x.ServiceName, x.Type, x.Temporality, x.IsMonotonic, x.Unit,
				x.ResourceID, x.Attributes, now}
		})
}

func (r *Repository) SaveSamples(ctx context.Context, rows []domain.Sample) error {
	return insert(ctx, r.conn,
		"INSERT INTO metric_samples (series_id, timestamp, value)",
		rows, func(x *domain.Sample) []any {
			return []any{x.SeriesID, x.Timestamp, x.Value}
		})
}

// insert sends rows as one native-protocol batch. Large batches mean fewer
// parts and fewer background merges in ClickHouse.
func insert[T any](ctx context.Context, conn driver.Conn, query string, rows []T, values func(*T) []any) error {
	b, err := conn.PrepareBatch(ctx, query)
	if err != nil {
		return err
	}
	for i := range rows {
		if err := b.Append(values(&rows[i])...); err != nil {
			_ = b.Abort()
			return err
		}
	}
	return b.Send()
}
