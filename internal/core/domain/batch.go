package domain

// Signal is one of the three OpenTelemetry signal types.
type Signal string

const (
	SignalLogs    Signal = "logs"
	SignalTraces  Signal = "traces"
	SignalMetrics Signal = "metrics"
)

func (s Signal) Valid() bool {
	return s == SignalLogs || s == SignalTraces || s == SignalMetrics
}

// Batch is decoded telemetry on its way through the write pipeline.
type Batch struct {
	Resources []Resource
	Logs      []LogRow
	Spans     []SpanRow
	Series    []Series
	Samples   []Sample
}

// Rows counts the fact rows; dimension rows (resources, series) are not counted.
func (b *Batch) Rows() int { return len(b.Logs) + len(b.Spans) + len(b.Samples) }
