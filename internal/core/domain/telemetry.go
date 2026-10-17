// Package domain holds the telemetry entities shared by every layer.
package domain

import "time"

// Span kinds and status codes, matching the Enum8 labels in the schema.
const (
	KindUnspecified = "Unspecified"
	KindInternal    = "Internal"
	KindServer      = "Server"
	KindClient      = "Client"
	KindProducer    = "Producer"
	KindConsumer    = "Consumer"

	StatusUnset = "Unset"
	StatusOk    = "Ok"
	StatusError = "Error"
)

// Resource is a deduplicated set of resource attributes. Rows reference it by
// ID instead of repeating host/pod/version attributes on every line.
type Resource struct {
	ID          uint64
	ServiceName string
	Attributes  map[string]string
}

type LogRow struct {
	Timestamp      time.Time
	ServiceName    string
	SeverityNumber uint8
	SeverityText   string
	TraceID        [16]byte
	SpanID         [8]byte
	ResourceID     uint64
	ScopeName      string
	Body           string
	Attributes     map[string]string
}

type SpanRow struct {
	Timestamp     time.Time
	TraceID       [16]byte
	SpanID        [8]byte
	ParentSpanID  [8]byte
	ServiceName   string
	Name          string
	Kind          string
	StatusCode    string
	StatusMessage string
	DurationNs    uint64
	ResourceID    uint64
	ScopeName     string

	// Hot attributes promoted out of Attributes into their own columns.
	HTTPMethod string
	HTTPRoute  string
	HTTPStatus uint16

	Attributes map[string]string
	EventTimes []time.Time
	EventNames []string
	EventAttrs []map[string]string
}

// IsEntry reports whether the span is where a request enters a service.
// RED metrics are computed from entry spans only.
func (s *SpanRow) IsEntry() bool {
	return s.Kind == KindServer || s.Kind == KindConsumer || s.ParentSpanID == [8]byte{}
}

func (s *SpanRow) IsError() bool { return s.StatusCode == StatusError }

// Stat returns the fields RED metrics are computed from.
func (s *SpanRow) Stat() SpanStat {
	return SpanStat{
		Timestamp:   s.Timestamp,
		ServiceName: s.ServiceName,
		Name:        s.Name,
		StatusCode:  s.StatusCode,
		DurationNs:  s.DurationNs,
	}
}

// SpanStat is an entry span reduced to what RED metrics need. Stats are taken
// before sampling so request rates and latencies stay exact.
type SpanStat struct {
	Timestamp   time.Time
	ServiceName string
	Name        string
	StatusCode  string
	DurationNs  uint64
}

// Series is one metric time series (name + attributes + resource).
type Series struct {
	ID          uint64
	MetricName  string
	ServiceName string
	Type        string // gauge | sum | histogram | summary
	Temporality string // unspecified | delta | cumulative
	IsMonotonic bool
	Unit        string
	ResourceID  uint64
	Attributes  map[string]string
}

type Sample struct {
	SeriesID  uint64
	Timestamp time.Time
	Value     float64
}
