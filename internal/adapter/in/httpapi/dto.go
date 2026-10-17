package httpapi

import (
	"encoding/hex"
	"maps"
	"strconv"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

// JSON shapes for the UI. Times are RFC 3339 strings or epoch milliseconds;
// nanosecond cursors travel as strings because JSON numbers in JavaScript
// cannot hold them exactly.

type logDTO struct {
	Time       string            `json:"time"`
	Cursor     string            `json:"ts"`
	Service    string            `json:"service"`
	Severity   string            `json:"severity"`
	Level      string            `json:"level"`
	TraceID    string            `json:"trace_id,omitempty"`
	SpanID     string            `json:"span_id,omitempty"`
	Body       string            `json:"body"`
	Attributes map[string]string `json:"attributes"`
	Resource   map[string]string `json:"resource"`
}

type logPageDTO struct {
	Logs []logDTO `json:"logs"`
	Next string   `json:"next,omitempty"`
}

type bucketDTO struct {
	T     int64  `json:"t"`
	Debug uint64 `json:"debug"`
	Info  uint64 `json:"info"`
	Warn  uint64 `json:"warn"`
	Error uint64 `json:"error"`
}

type histogramDTO struct {
	StepMs  int64       `json:"step_ms"`
	Buckets []bucketDTO `json:"buckets"`
}

type latencyDTO struct {
	P50 float64 `json:"p50_ms"`
	P95 float64 `json:"p95_ms"`
	P99 float64 `json:"p99_ms"`
}

type serviceDTO struct {
	Name      string  `json:"name"`
	Requests  uint64  `json:"requests"`
	Errors    uint64  `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
	latencyDTO
	Logs      uint64 `json:"logs"`
	LogErrors uint64 `json:"log_errors"`
}

type operationDTO struct {
	Name      string  `json:"name"`
	Requests  uint64  `json:"requests"`
	Errors    uint64  `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
	latencyDTO
}

type traceSummaryDTO struct {
	TraceID    string  `json:"trace_id"`
	Service    string  `json:"service"`
	Name       string  `json:"name"`
	Time       string  `json:"time"`
	DurationMs float64 `json:"duration_ms"`
	Status     string  `json:"status"`
	HTTPMethod string  `json:"http_method,omitempty"`
	HTTPRoute  string  `json:"http_route,omitempty"`
	HTTPStatus uint16  `json:"http_status,omitempty"`
}

type eventDTO struct {
	OffsetMs   float64           `json:"offset_ms"`
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes"`
}

type spanDTO struct {
	SpanID        string            `json:"span_id"`
	ParentID      string            `json:"parent_id,omitempty"`
	Service       string            `json:"service"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	Status        string            `json:"status"`
	StatusMessage string            `json:"status_message,omitempty"`
	OffsetMs      float64           `json:"offset_ms"`
	DurationMs    float64           `json:"duration_ms"`
	Attributes    map[string]string `json:"attributes"`
	Resource      map[string]string `json:"resource"`
	Events        []eventDTO        `json:"events"`
}

type traceDTO struct {
	TraceID    string    `json:"trace_id"`
	Time       string    `json:"time"`
	DurationMs float64   `json:"duration_ms"`
	Services   []string  `json:"services"`
	Spans      []spanDTO `json:"spans"`
	Logs       []logDTO  `json:"logs"`
}

var levelNames = map[domain.SeverityLevel]string{
	domain.LevelDebug: "debug", domain.LevelInfo: "info", domain.LevelWarn: "warn", domain.LevelError: "error",
}

func idString(b []byte) string {
	for _, c := range b {
		if c != 0 {
			return hex.EncodeToString(b)
		}
	}
	return ""
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func rate(errors, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(errors) / float64(total)
}

func toLogDTO(e domain.LogEntry) logDTO {
	return logDTO{
		Time:       rfc3339(e.Timestamp),
		Cursor:     strconv.FormatInt(e.Timestamp.UnixNano(), 10),
		Service:    e.ServiceName,
		Severity:   e.SeverityText,
		Level:      levelNames[domain.LevelOf(e.SeverityNumber)],
		TraceID:    idString(e.TraceID[:]),
		SpanID:     idString(e.SpanID[:]),
		Body:       e.Body,
		Attributes: e.Attributes,
		Resource:   e.Resource,
	}
}

func toLogDTOs(entries []domain.LogEntry) []logDTO {
	out := make([]logDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, toLogDTO(e))
	}
	return out
}

func toLatencyDTO(l domain.Latency) latencyDTO { return latencyDTO{P50: l.P50, P95: l.P95, P99: l.P99} }

// spanAttributes puts the promoted HTTP columns back, so the UI shows the
// span's attributes as the application sent them.
func spanAttributes(s domain.SpanRow) map[string]string {
	attrs := maps.Clone(s.Attributes)
	if attrs == nil {
		attrs = map[string]string{}
	}
	if s.HTTPMethod != "" {
		attrs["http.request.method"] = s.HTTPMethod
	}
	if s.HTTPRoute != "" {
		attrs["http.route"] = s.HTTPRoute
	}
	if s.HTTPStatus != 0 {
		attrs["http.response.status_code"] = strconv.Itoa(int(s.HTTPStatus))
	}
	return attrs
}

func toTraceDTO(t domain.Trace) traceDTO {
	start, end := t.Spans[0].Timestamp, t.Spans[0].Timestamp
	for _, s := range t.Spans {
		if s.Timestamp.Before(start) {
			start = s.Timestamp
		}
		if e := s.Timestamp.Add(time.Duration(s.DurationNs)); e.After(end) {
			end = e
		}
	}

	var services []string
	seen := map[string]bool{}
	spans := make([]spanDTO, 0, len(t.Spans))
	for _, s := range t.Spans {
		if !seen[s.ServiceName] {
			seen[s.ServiceName] = true
			services = append(services, s.ServiceName)
		}
		events := make([]eventDTO, 0, len(s.EventNames))
		for i, name := range s.EventNames {
			events = append(events, eventDTO{
				OffsetMs:   ms(s.EventTimes[i].Sub(start)),
				Name:       name,
				Attributes: s.EventAttrs[i],
			})
		}
		spans = append(spans, spanDTO{
			SpanID:        idString(s.SpanID[:]),
			ParentID:      idString(s.ParentSpanID[:]),
			Service:       s.ServiceName,
			Name:          s.Name,
			Kind:          s.Kind,
			Status:        s.StatusCode,
			StatusMessage: s.StatusMessage,
			OffsetMs:      ms(s.Timestamp.Sub(start)),
			DurationMs:    ms(time.Duration(s.DurationNs)),
			Attributes:    spanAttributes(s),
			Resource:      t.Resources[s.ResourceID],
			Events:        events,
		})
	}
	return traceDTO{
		TraceID:    hex.EncodeToString(t.ID[:]),
		Time:       rfc3339(start),
		DurationMs: ms(end.Sub(start)),
		Services:   services,
		Spans:      spans,
		Logs:       toLogDTOs(t.Logs),
	}
}
