package domain

import (
	"fmt"
	"time"
)

// Aggregation reduces the samples in one time bucket to a value.
type Aggregation string

const (
	AggAvg   Aggregation = "avg"
	AggSum   Aggregation = "sum"
	AggMin   Aggregation = "min"
	AggMax   Aggregation = "max"
	AggCount Aggregation = "count"
	AggLast  Aggregation = "last"
	// AggRate is the per-second increase of a counter.
	AggRate Aggregation = "rate"
)

func (a Aggregation) Valid() bool {
	switch a {
	case AggAvg, AggSum, AggMin, AggMax, AggCount, AggLast, AggRate:
		return true
	}
	return false
}

// MetricQuery selects series by name, service and attribute filters, and
// optionally merges them by one attribute.
type MetricQuery struct {
	Name    string            `json:"name"`
	Service string            `json:"service,omitempty"`
	Filters map[string]string `json:"filters,omitempty"`
	Agg     Aggregation       `json:"agg"`
	GroupBy string            `json:"group_by,omitempty"`
}

func (q MetricQuery) Validate() error {
	if q.Name == "" {
		return fmt.Errorf("%w: metric name is required", ErrInvalidQuery)
	}
	if !q.Agg.Valid() {
		return fmt.Errorf("%w: unknown aggregation %q", ErrInvalidQuery, q.Agg)
	}
	return nil
}

// Resolution is which table metric buckets come from.
type Resolution int

const (
	ResolutionRaw Resolution = iota
	Resolution1m
	Resolution1h
)

// SeriesInfo is what aggregation needs to know about one series.
type SeriesInfo struct {
	ID          uint64
	Attributes  map[string]string
	Monotonic   bool
	Temporality string
}

// MetricBucket summarises one series over one step.
type MetricBucket struct {
	SeriesID uint64
	Time     time.Time
	Min, Max float64
	Sum      float64
	Count    uint64
	Last     float64
}

type MetricName struct {
	Name string
	Type string
	Unit string
}

// MetricPoint has Valid false where a bucket had no data.
type MetricPoint struct {
	Time  time.Time
	Value float64
	Valid bool
}

type MetricSeries struct {
	// Group is the GroupBy attribute's value; empty when not grouped.
	Group  string
	Points []MetricPoint
}

type MetricResult struct {
	Step   time.Duration
	Series []MetricSeries
	// Truncated is set when more series matched than are returned.
	Truncated bool
}

// REDPoint is request rate, errors and latency of one step.
type REDPoint struct {
	Time     time.Time
	Requests uint64
	Errors   uint64
	Latency  Latency
}
