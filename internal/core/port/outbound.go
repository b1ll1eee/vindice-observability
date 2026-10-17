package port

import (
	"context"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

// Publisher durably enqueues raw OTLP payloads for the writer.
type Publisher interface {
	Publish(ctx context.Context, sig domain.Signal, payload []byte) error
}

// The repositories below are segregated by aggregate (Repository pattern).

type ResourceRepository interface {
	SaveResources(ctx context.Context, rows []domain.Resource) error
}

type LogRepository interface {
	SaveLogs(ctx context.Context, rows []domain.LogRow) error
}

type TraceRepository interface {
	SaveSpans(ctx context.Context, rows []domain.SpanRow) error
	SaveSpanStats(ctx context.Context, rows []domain.SpanStat) error
}

type MetricRepository interface {
	SaveSeries(ctx context.Context, rows []domain.Series) error
	SaveSamples(ctx context.Context, rows []domain.Sample) error
}

type TelemetryRepository interface {
	ResourceRepository
	LogRepository
	TraceRepository
	MetricRepository
}

// TelemetryReader is the read-side repository. It is kept apart from the
// write repositories so reads can be optimised (rollups, caching) on their own.
type TelemetryReader interface {
	ServiceRED(ctx context.Context, r domain.TimeRange) ([]domain.ServiceSummary, error)
	ServiceLogCounts(ctx context.Context, r domain.TimeRange) (map[string]domain.LogCount, error)
	OperationRED(ctx context.Context, service string, r domain.TimeRange) ([]domain.OperationSummary, error)
	SearchLogs(ctx context.Context, q domain.LogQuery) ([]domain.LogEntry, error)
	// LogHistogram buckets log counts by step. rollup selects the per-minute
	// table, which is only valid without content filters and with step >= 1m.
	LogHistogram(ctx context.Context, q domain.LogQuery, step time.Duration, rollup bool) ([]domain.HistogramBucket, error)
	SearchTraces(ctx context.Context, q domain.TraceQuery) ([]domain.TraceSummary, error)
	// TraceWindow returns when the trace's spans started; ok is false if unknown.
	TraceWindow(ctx context.Context, id [16]byte) (r domain.TimeRange, ok bool, err error)
	TraceSpans(ctx context.Context, id [16]byte, r domain.TimeRange) ([]domain.SpanRow, error)
	TraceLogs(ctx context.Context, id [16]byte, r domain.TimeRange) ([]domain.LogEntry, error)
	Resources(ctx context.Context, ids []uint64) (map[uint64]map[string]string, error)
}

// SchemaMigrator creates or updates the storage schema.
type SchemaMigrator interface {
	Migrate(ctx context.Context) error
}
