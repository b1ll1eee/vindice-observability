// Package port defines the interfaces between the core and the adapters.
// Inbound ports are implemented by core services and called by driving
// adapters; outbound ports are implemented by driven adapters.
package port

import (
	"context"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

// Ingestor accepts raw OTLP protobuf export requests (from HTTP and gRPC).
type Ingestor interface {
	Ingest(ctx context.Context, sig domain.Signal, payload []byte) error
}

// Querier answers the read-side questions the UI asks.
type Querier interface {
	Services(ctx context.Context, r domain.TimeRange) ([]domain.ServiceSummary, error)
	Operations(ctx context.Context, service string, r domain.TimeRange) ([]domain.OperationSummary, error)
	SearchLogs(ctx context.Context, q domain.LogQuery) (domain.LogPage, error)
	LogHistogram(ctx context.Context, q domain.LogQuery) (domain.Histogram, error)
	SearchTraces(ctx context.Context, q domain.TraceQuery) ([]domain.TraceSummary, error)
	Trace(ctx context.Context, id [16]byte) (domain.Trace, error)
}

// BatchWriter buffers decoded telemetry and persists it on Flush.
// It is driven by the queue consumer, which commits offsets only after a
// successful Flush.
type BatchWriter interface {
	Add(b domain.Batch, now time.Time)
	// Buffered returns the number of rows waiting to be written.
	Buffered() int
	// Flush writes everything that is ready. final also flushes rows that are
	// still being held back (such as traces awaiting a sampling decision).
	// A failed Flush keeps the rows that were not written, so it can be retried.
	Flush(ctx context.Context, now time.Time, final bool) error
}
