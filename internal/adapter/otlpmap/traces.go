package otlpmap

import (
	"time"

	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

func MapTraces(req *coltrace.ExportTraceServiceRequest) ([]domain.SpanRow, []domain.Resource) {
	var (
		rows []domain.SpanRow
		res  resources
	)
	for _, rs := range req.GetResourceSpans() {
		r := res.add(rs.GetResource())
		for _, ss := range rs.GetScopeSpans() {
			scope := ss.GetScope().GetName()
			for _, sp := range ss.GetSpans() {
				start, end := sp.GetStartTimeUnixNano(), sp.GetEndTimeUnixNano()
				var dur uint64
				if end > start {
					dur = end - start
				}
				events := sp.GetEvents()
				row := domain.SpanRow{
					Timestamp:     unixNano(start),
					TraceID:       traceID(sp.GetTraceId()),
					SpanID:        spanID(sp.GetSpanId()),
					ParentSpanID:  spanID(sp.GetParentSpanId()),
					ServiceName:   r.ServiceName,
					Name:          sp.GetName(),
					Kind:          spanKind(sp.GetKind()),
					StatusCode:    statusCode(sp.GetStatus().GetCode()),
					StatusMessage: sp.GetStatus().GetMessage(),
					DurationNs:    dur,
					ResourceID:    r.ID,
					ScopeName:     scope,
					Attributes:    attrMap(sp.GetAttributes()),
					EventTimes:    make([]time.Time, 0, len(events)),
					EventNames:    make([]string, 0, len(events)),
					EventAttrs:    make([]map[string]string, 0, len(events)),
				}
				for _, ev := range events {
					row.EventTimes = append(row.EventTimes, unixNano(ev.GetTimeUnixNano()))
					row.EventNames = append(row.EventNames, ev.GetName())
					row.EventAttrs = append(row.EventAttrs, attrMap(ev.GetAttributes()))
				}
				rows = append(rows, row)
			}
		}
	}
	return rows, res.list
}

func spanKind(k tracepb.Span_SpanKind) string {
	switch k {
	case tracepb.Span_SPAN_KIND_INTERNAL:
		return domain.KindInternal
	case tracepb.Span_SPAN_KIND_SERVER:
		return domain.KindServer
	case tracepb.Span_SPAN_KIND_CLIENT:
		return domain.KindClient
	case tracepb.Span_SPAN_KIND_PRODUCER:
		return domain.KindProducer
	case tracepb.Span_SPAN_KIND_CONSUMER:
		return domain.KindConsumer
	}
	return domain.KindUnspecified
}

func statusCode(c tracepb.Status_StatusCode) string {
	switch c {
	case tracepb.Status_STATUS_CODE_OK:
		return domain.StatusOk
	case tracepb.Status_STATUS_CODE_ERROR:
		return domain.StatusError
	}
	return domain.StatusUnset
}
