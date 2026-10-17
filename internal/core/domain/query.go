package domain

import (
	"encoding/hex"
	"fmt"
	"time"
)

// TimeRange is half-open: [From, To).
type TimeRange struct {
	From, To time.Time
}

// LogQuery is the criteria for searching logs (Query Object pattern).
type LogQuery struct {
	Range   TimeRange
	Service string
	// MinSeverity keeps logs at this severity number or above; logs with no
	// severity count as INFO (9).
	MinSeverity uint8
	Text        string
	TraceID     *[16]byte
	// Before is a keyset cursor: only logs strictly older are returned.
	Before time.Time
	Limit  int
}

// HasContentFilter reports whether the query filters on log content, which
// rules out the pre-aggregated per-minute counts.
func (q LogQuery) HasContentFilter() bool { return q.Text != "" || q.TraceID != nil }

type LogEntry struct {
	Timestamp      time.Time
	ServiceName    string
	SeverityNumber uint8
	SeverityText   string
	TraceID        [16]byte
	SpanID         [8]byte
	Body           string
	Attributes     map[string]string
	Resource       map[string]string
}

type LogPage struct {
	Entries []LogEntry
	// Next is the cursor for the following page; zero when there is none.
	Next time.Time
}

// SeverityLevel groups severity numbers the way the UI colours them.
type SeverityLevel int

const (
	LevelDebug SeverityLevel = iota // 1-8
	LevelInfo                       // 9-12 and unknown (0)
	LevelWarn                       // 13-16
	LevelError                      // 17+
)

func LevelOf(n uint8) SeverityLevel {
	switch {
	case n >= 17:
		return LevelError
	case n >= 13:
		return LevelWarn
	case n >= 9 || n == 0:
		return LevelInfo
	}
	return LevelDebug
}

type HistogramBucket struct {
	Time                      time.Time
	Debug, Info, Warn, Errors uint64
}

type Histogram struct {
	Step    time.Duration
	Buckets []HistogramBucket
}

// Latency holds percentiles in milliseconds.
type Latency struct {
	P50, P95, P99 float64
}

type ServiceSummary struct {
	Name      string
	Requests  uint64
	Errors    uint64
	Latency   Latency
	Logs      uint64
	LogErrors uint64
}

type LogCount struct {
	Total, Errors uint64
}

type OperationSummary struct {
	Name     string
	Requests uint64
	Errors   uint64
	Latency  Latency
}

type TraceQuery struct {
	Range       TimeRange
	Service     string
	MinDuration time.Duration
	// ErrorsOnly keeps traces where any span failed, not only the root.
	ErrorsOnly bool
	Limit      int
}

// TraceSummary describes a trace by its root span.
type TraceSummary struct {
	TraceID     [16]byte
	ServiceName string
	Name        string
	Timestamp   time.Time
	DurationNs  uint64
	StatusCode  string
	HTTPMethod  string
	HTTPRoute   string
	HTTPStatus  uint16
}

// Trace is one trace with everything needed to render it: spans, the logs
// emitted inside it, and the resources both refer to.
type Trace struct {
	ID        [16]byte
	Spans     []SpanRow
	Logs      []LogEntry
	Resources map[uint64]map[string]string
}

func ParseTraceID(s string) ([16]byte, error) {
	var id [16]byte
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(id) {
		return id, fmt.Errorf("%w: trace id must be 32 hex characters", ErrInvalidQuery)
	}
	copy(id[:], b)
	return id, nil
}
