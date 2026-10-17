package domain

import (
	"fmt"
	"time"
)

type Operator string

const (
	OpGT Operator = ">"
	OpGE Operator = ">="
	OpLT Operator = "<"
	OpLE Operator = "<="
)

func (o Operator) Breached(value, threshold float64) bool {
	switch o {
	case OpGT:
		return value > threshold
	case OpGE:
		return value >= threshold
	case OpLT:
		return value < threshold
	case OpLE:
		return value <= threshold
	}
	return false
}

// AlertRule fires when Signal, aggregated over Window, compares to Threshold
// by Op for at least For.
type AlertRule struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Enabled   bool          `json:"enabled"`
	Signal    SignalQuery   `json:"signal"`
	Window    time.Duration `json:"window"`
	Op        Operator      `json:"op"`
	Threshold float64       `json:"threshold"`
	For       time.Duration `json:"for"`
	// ChatID overrides the default Telegram chat.
	ChatID    string    `json:"chat_id,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r AlertRule) Validate() error {
	switch {
	case r.Name == "":
		return fmt.Errorf("%w: name is required", ErrInvalidQuery)
	case r.Window < time.Minute || r.Window > 24*time.Hour:
		return fmt.Errorf("%w: window must be between 1m and 24h", ErrInvalidQuery)
	case r.For < 0 || r.For > 24*time.Hour:
		return fmt.Errorf("%w: for must be between 0 and 24h", ErrInvalidQuery)
	}
	switch r.Op {
	case OpGT, OpGE, OpLT, OpLE:
	default:
		return fmt.Errorf("%w: unknown operator %q", ErrInvalidQuery, r.Op)
	}
	return r.Signal.Validate()
}

type AlertState string

const (
	AlertOK      AlertState = "ok"
	AlertPending AlertState = "pending"
	AlertFiring  AlertState = "firing"
)

// AlertStatus is the evaluator's memory of one rule.
type AlertStatus struct {
	RuleID    string     `json:"rule_id"`
	State     AlertState `json:"state"`
	Since     time.Time  `json:"since"`
	Value     float64    `json:"value"`
	EvalAt    time.Time  `json:"eval_at"`
	Error     string     `json:"error,omitempty"`
	Breaching []string   `json:"breaching,omitempty"`
}

// AlertEvent records a notification-worthy transition.
type AlertEvent struct {
	RuleID   string
	RuleName string
	Time     time.Time
	// State is firing or resolved.
	State   string
	Value   float64
	Message string
}

const (
	EventFiring   = "firing"
	EventResolved = "resolved"
	EventTest     = "test"
)

// Notification is what a Notifier delivers.
type Notification struct {
	Event     string
	Rule      AlertRule
	Value     float64
	Breaching []string
	URL       string
}
