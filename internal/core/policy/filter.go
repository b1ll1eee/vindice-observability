// Package policy holds the domain rules applied to telemetry before storage:
// drop rules (specifications), attribute promotion, tail sampling (strategy)
// and dimension dedupe.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

// Spec is a predicate over a row (Specification pattern). Rules are built by
// combining small specs with allOf, and a filter is anyOf its rules.
type Spec[T any] func(*T) bool

func allOf[T any](specs ...Spec[T]) Spec[T] {
	return func(v *T) bool {
		for _, s := range specs {
			if !s(v) {
				return false
			}
		}
		return true
	}
}

func anyOf[T any](specs ...Spec[T]) Spec[T] {
	return func(v *T) bool {
		for _, s := range specs {
			if s(v) {
				return true
			}
		}
		return false
	}
}

// Rule drops matching rows. Every field that is set must match.
type Rule struct {
	Signal    domain.Signal `json:"signal"`              // logs | traces | metrics
	Service   string        `json:"service,omitempty"`   // exact service.name
	Name      string        `json:"name,omitempty"`      // regex on span or metric name
	Body      string        `json:"body,omitempty"`      // regex on log body
	Attribute string        `json:"attribute,omitempty"` // attribute key ...
	Value     string        `json:"value,omitempty"`     // ... that must equal Value
	// MaxSeverity drops logs whose severity number is 1..MaxSeverity (logs only).
	MaxSeverity uint8 `json:"max_severity,omitempty"`
}

// Filter decides which rows are dropped before storage.
type Filter struct {
	DropLog    Spec[domain.LogRow]
	DropSpan   Spec[domain.SpanRow]
	DropSeries Spec[domain.Series]
}

// LoadFilter reads {"drop": [...rules]} from path. An empty path means no rules.
func LoadFilter(path string) (*Filter, error) {
	if path == "" {
		return NewFilter(nil)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file struct {
		Drop []Rule `json:"drop"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return NewFilter(file.Drop)
}

func NewFilter(rules []Rule) (*Filter, error) {
	var (
		logs    []Spec[domain.LogRow]
		spans   []Spec[domain.SpanRow]
		metrics []Spec[domain.Series]
	)
	for i, r := range rules {
		name, err := compile(r.Name)
		if err != nil {
			return nil, fmt.Errorf("rule %d: name: %w", i, err)
		}
		body, err := compile(r.Body)
		if err != nil {
			return nil, fmt.Errorf("rule %d: body: %w", i, err)
		}
		switch r.Signal {
		case domain.SignalLogs:
			logs = append(logs, allOf(
				func(l *domain.LogRow) bool { return r.Service == "" || r.Service == l.ServiceName },
				func(l *domain.LogRow) bool { return body == nil || body.MatchString(l.Body) },
				func(l *domain.LogRow) bool {
					return r.MaxSeverity == 0 || (l.SeverityNumber != 0 && l.SeverityNumber <= r.MaxSeverity)
				},
				func(l *domain.LogRow) bool { return attrEquals(r, l.Attributes) },
			))
		case domain.SignalTraces:
			spans = append(spans, allOf(
				func(s *domain.SpanRow) bool { return r.Service == "" || r.Service == s.ServiceName },
				func(s *domain.SpanRow) bool { return name == nil || name.MatchString(s.Name) },
				func(s *domain.SpanRow) bool { return attrEquals(r, s.Attributes) },
			))
		case domain.SignalMetrics:
			metrics = append(metrics, allOf(
				func(s *domain.Series) bool { return r.Service == "" || r.Service == s.ServiceName },
				func(s *domain.Series) bool { return name == nil || name.MatchString(s.MetricName) },
				func(s *domain.Series) bool { return attrEquals(r, s.Attributes) },
			))
		default:
			return nil, fmt.Errorf("rule %d: unknown signal %q", i, r.Signal)
		}
	}
	return &Filter{DropLog: anyOf(logs...), DropSpan: anyOf(spans...), DropSeries: anyOf(metrics...)}, nil
}

func compile(expr string) (*regexp.Regexp, error) {
	if expr == "" {
		return nil, nil
	}
	return regexp.Compile(expr)
}

func attrEquals(r Rule, attrs map[string]string) bool {
	if r.Attribute == "" {
		return true
	}
	v, ok := attrs[r.Attribute]
	return ok && v == r.Value
}
