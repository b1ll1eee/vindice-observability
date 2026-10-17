package otlpmap

import (
	"time"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

// MapLogs flattens an export request. now is used when a record has no timestamp.
func MapLogs(req *collogs.ExportLogsServiceRequest, now time.Time) ([]domain.LogRow, []domain.Resource) {
	var (
		rows []domain.LogRow
		res  resources
	)
	for _, rl := range req.GetResourceLogs() {
		r := res.add(rl.GetResource())
		for _, sl := range rl.GetScopeLogs() {
			scope := sl.GetScope().GetName()
			for _, lr := range sl.GetLogRecords() {
				ts := now
				if n := lr.GetTimeUnixNano(); n != 0 {
					ts = unixNano(n)
				} else if n := lr.GetObservedTimeUnixNano(); n != 0 {
					ts = unixNano(n)
				}
				num, text := NormalizeSeverity(uint8(lr.GetSeverityNumber()), lr.GetSeverityText())
				rows = append(rows, domain.LogRow{
					Timestamp:      ts,
					ServiceName:    r.ServiceName,
					SeverityNumber: num,
					SeverityText:   text,
					TraceID:        traceID(lr.GetTraceId()),
					SpanID:         spanID(lr.GetSpanId()),
					ResourceID:     r.ID,
					ScopeName:      scope,
					Body:           AnyValueString(lr.GetBody()),
					Attributes:     attrMap(lr.GetAttributes()),
				})
			}
		}
	}
	return rows, res.list
}
