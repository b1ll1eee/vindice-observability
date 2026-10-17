package otlpmap

import (
	"fmt"
	"time"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

// Decode unmarshals an OTLP protobuf export request and maps it to a batch.
// now stands in for missing log timestamps. skipped counts data points of
// unsupported types.
func Decode(sig domain.Signal, payload []byte, now time.Time) (b domain.Batch, skipped int, err error) {
	switch sig {
	case domain.SignalLogs:
		req := &collogs.ExportLogsServiceRequest{}
		if err = proto.Unmarshal(payload, req); err == nil {
			b.Logs, b.Resources = MapLogs(req, now)
		}
	case domain.SignalTraces:
		req := &coltrace.ExportTraceServiceRequest{}
		if err = proto.Unmarshal(payload, req); err == nil {
			b.Spans, b.Resources = MapTraces(req)
		}
	case domain.SignalMetrics:
		req := &colmetrics.ExportMetricsServiceRequest{}
		if err = proto.Unmarshal(payload, req); err == nil {
			m := MapMetrics(req)
			b.Series, b.Samples, b.Resources, skipped = m.Series, m.Samples, m.Resources, m.Skipped
		}
	default:
		err = fmt.Errorf("%w: %q", domain.ErrInvalidSignal, sig)
	}
	return b, skipped, err
}
