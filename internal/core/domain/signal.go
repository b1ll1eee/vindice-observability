package domain

import (
	"fmt"
	"slices"
)

// SignalKind is what a dashboard panel shows or an alert rule watches.
type SignalKind string

const (
	SignalKindMetric SignalKind = "metric"
	SignalKindRED    SignalKind = "red"
	SignalKindLogs   SignalKind = "logs"
)

// RED measures.
const (
	MeasureRequests  = "requests"
	MeasureErrors    = "errors"
	MeasureErrorRate = "error_rate"
	MeasureP50       = "p50"
	MeasureP95       = "p95"
	MeasureP99       = "p99"
)

var redMeasures = []string{MeasureRequests, MeasureErrors, MeasureErrorRate, MeasureP50, MeasureP95, MeasureP99}

// SignalQuery is a query shared by dashboard panels and alert rules. Exactly the
// fields of its Kind are used.
type SignalQuery struct {
	Kind SignalKind `json:"kind"`

	// metric
	Metric *MetricQuery `json:"metric,omitempty"`

	// red, logs
	Service string `json:"service,omitempty"`
	// red: one of the RED measures
	Measure string `json:"measure,omitempty"`

	// logs: count of logs matching
	MinSeverity uint8  `json:"min_severity,omitempty"`
	Text        string `json:"text,omitempty"`
}

func (s SignalQuery) Validate() error {
	switch s.Kind {
	case SignalKindMetric:
		if s.Metric == nil {
			return fmt.Errorf("%w: metric signal needs a metric query", ErrInvalidQuery)
		}
		return s.Metric.Validate()
	case SignalKindRED:
		if s.Service == "" {
			return fmt.Errorf("%w: red signal needs a service", ErrInvalidQuery)
		}
		if !slices.Contains(redMeasures, s.Measure) {
			return fmt.Errorf("%w: unknown red measure %q", ErrInvalidQuery, s.Measure)
		}
	case SignalKindLogs:
	default:
		return fmt.Errorf("%w: unknown signal kind %q", ErrInvalidQuery, s.Kind)
	}
	return nil
}

// REDValue extracts a measure from aggregated RED figures.
func REDValue(measure string, requests, errors uint64, l Latency) float64 {
	switch measure {
	case MeasureRequests:
		return float64(requests)
	case MeasureErrors:
		return float64(errors)
	case MeasureErrorRate:
		if requests == 0 {
			return 0
		}
		return float64(errors) / float64(requests)
	case MeasureP50:
		return l.P50
	case MeasureP95:
		return l.P95
	case MeasureP99:
		return l.P99
	}
	return 0
}
