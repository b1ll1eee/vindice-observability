package port

import (
	"context"

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

// SchemaMigrator creates or updates the storage schema.
type SchemaMigrator interface {
	Migrate(ctx context.Context) error
}
