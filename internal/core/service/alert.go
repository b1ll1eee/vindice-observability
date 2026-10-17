package service

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

// SignalEvaluator reduces a signal over a time range to per-group values.
// QueryService implements it.
type SignalEvaluator interface {
	SignalValues(ctx context.Context, sig domain.SignalQuery, r domain.TimeRange) ([]GroupValue, error)
}

// AlertService manages rules and evaluates them. Only one evaluator should
// run at a time (one process with -mode=all or -mode=alert), otherwise
// notifications are sent twice.
type AlertService struct {
	rules     port.AlertRuleRepository
	state     port.AlertStateRepository
	signals   SignalEvaluator
	notifier  port.Notifier
	publicURL string
	log       *slog.Logger
	now       func() time.Time

	mu         sync.Mutex
	statuses   map[string]domain.AlertStatus // nil until loaded
	evaluating bool                          // set by Run
}

var _ port.AlertManager = (*AlertService)(nil)

type AlertDeps struct {
	Rules     port.AlertRuleRepository
	State     port.AlertStateRepository
	Signals   SignalEvaluator
	Notifier  port.Notifier
	PublicURL string
	Log       *slog.Logger
}

func NewAlertService(d AlertDeps) *AlertService {
	return &AlertService{
		rules: d.Rules, state: d.State, signals: d.Signals, notifier: d.Notifier,
		publicURL: strings.TrimRight(d.PublicURL, "/"), log: d.Log, now: time.Now,
	}
}

func (a *AlertService) ListRules(ctx context.Context) ([]domain.AlertRule, error) {
	return a.rules.ListRules(ctx)
}

func (a *AlertService) GetRule(ctx context.Context, id string) (domain.AlertRule, error) {
	r, ok, err := a.rules.GetRule(ctx, id)
	if err == nil && !ok {
		err = domain.ErrNotFound
	}
	return r, err
}

func (a *AlertService) SaveRule(ctx context.Context, r domain.AlertRule) (domain.AlertRule, error) {
	if err := r.Validate(); err != nil {
		return r, err
	}
	if r.ID == "" {
		r.ID = newID()
	} else if _, err := a.GetRule(ctx, r.ID); err != nil {
		return r, err
	}
	r.UpdatedAt = a.now().UTC()
	return r, a.rules.SaveRule(ctx, r)
}

func (a *AlertService) DeleteRule(ctx context.Context, id string) error {
	if _, err := a.GetRule(ctx, id); err != nil {
		return err
	}
	return a.rules.DeleteRule(ctx, id)
}

func (a *AlertService) Statuses(ctx context.Context) (map[string]domain.AlertStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.evaluating {
		// Another process evaluates (-mode=alert); read its latest state.
		return a.state.LoadStatuses(ctx)
	}
	if err := a.load(ctx); err != nil {
		return nil, err
	}
	return maps.Clone(a.statuses), nil
}

func (a *AlertService) Events(ctx context.Context, ruleID string, limit int) ([]domain.AlertEvent, error) {
	return a.state.Events(ctx, ruleID, clampLimit(limit, 50, 500))
}

func (a *AlertService) TestRule(ctx context.Context, id string) error {
	r, err := a.GetRule(ctx, id)
	if err != nil {
		return err
	}
	value, breaching, err := a.evaluate(ctx, r, a.now())
	if err != nil {
		return err
	}
	return a.notifier.Notify(ctx, a.notification(domain.EventTest, r, value, breaching))
}

// Run evaluates every rule each interval until ctx ends.
func (a *AlertService) Run(ctx context.Context, interval time.Duration) error {
	a.mu.Lock()
	a.evaluating = true
	a.mu.Unlock()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := a.Evaluate(ctx); err != nil && ctx.Err() == nil {
			a.log.Error("alert evaluation failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Evaluate runs one round: every enabled rule moves through the state
// machine ok -> pending -> firing -> ok, notifying on firing and resolving.
func (a *AlertService) Evaluate(ctx context.Context) error {
	rules, err := a.rules.ListRules(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.load(ctx); err != nil {
		return err
	}

	now := a.now()
	live := map[string]bool{}
	var (
		changed []domain.AlertStatus
		events  []domain.AlertEvent
	)
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		live[r.ID] = true
		st, ok := a.statuses[r.ID]
		if !ok {
			st = domain.AlertStatus{RuleID: r.ID, State: domain.AlertOK, Since: now}
		}

		value, breaching, err := a.evaluate(ctx, r, now)
		st.EvalAt = now
		if err != nil {
			// Keep the current state: a failed query is not a recovery.
			st.Error = err.Error()
			a.statuses[r.ID] = st
			changed = append(changed, st)
			continue
		}
		st.Error, st.Value, st.Breaching = "", value, breaching

		next, event := transition(st, len(breaching) > 0, r.For, now)
		if event != "" {
			ev := domain.AlertEvent{RuleID: r.ID, RuleName: r.Name, Time: now, State: event, Value: value}
			if err := a.notifier.Notify(ctx, a.notification(event, r, value, breaching)); err != nil {
				a.log.Warn("alert notification failed", "rule", r.Name, "err", err)
				ev.Message = "notification failed: " + err.Error()
			}
			events = append(events, ev)
		}
		a.statuses[r.ID] = next
		changed = append(changed, next)
	}
	// Rules that were deleted or disabled are forgotten (no resolve message).
	for id := range a.statuses {
		if !live[id] {
			delete(a.statuses, id)
		}
	}

	if err := a.state.SaveStatuses(ctx, changed); err != nil {
		return err
	}
	return a.state.AppendEvents(ctx, events)
}

// transition is the alert state machine. It returns the new status and the
// event to notify ("" for none).
func transition(st domain.AlertStatus, breach bool, hold time.Duration, now time.Time) (domain.AlertStatus, string) {
	switch st.State {
	case domain.AlertFiring:
		if !breach {
			st.State, st.Since = domain.AlertOK, now
			return st, domain.EventResolved
		}
	case domain.AlertPending:
		switch {
		case !breach:
			st.State, st.Since = domain.AlertOK, now
		case now.Sub(st.Since) >= hold:
			st.State, st.Since = domain.AlertFiring, now
			return st, domain.EventFiring
		}
	default: // ok
		if breach {
			if hold == 0 {
				st.State, st.Since = domain.AlertFiring, now
				return st, domain.EventFiring
			}
			st.State, st.Since = domain.AlertPending, now
		}
	}
	return st, ""
}

// evaluate returns the rule's headline value and the groups that breach.
// The headline is the worst value: highest for > and >=, lowest for < and <=.
func (a *AlertService) evaluate(ctx context.Context, r domain.AlertRule, now time.Time) (float64, []string, error) {
	vals, err := a.signals.SignalValues(ctx, r.Signal, domain.TimeRange{From: now.Add(-r.Window), To: now})
	if err != nil {
		return 0, nil, err
	}
	high := r.Op == domain.OpGT || r.Op == domain.OpGE
	var (
		worst     float64
		breaching []string
	)
	for i, v := range vals {
		if i == 0 || (high && v.Value > worst) || (!high && v.Value < worst) {
			worst = v.Value
		}
		if r.Op.Breached(v.Value, r.Threshold) {
			name := v.Group
			if name == "" {
				name = r.Name
			}
			breaching = append(breaching, fmt.Sprintf("%s = %s", name, formatValue(v.Value)))
		}
	}
	return worst, breaching, nil
}

func (a *AlertService) notification(event string, r domain.AlertRule, value float64, breaching []string) domain.Notification {
	url := ""
	if a.publicURL != "" {
		url = a.publicURL + "/alerts"
	}
	return domain.Notification{Event: event, Rule: r, Value: value, Breaching: breaching, URL: url}
}

func (a *AlertService) load(ctx context.Context) error {
	if a.statuses != nil {
		return nil
	}
	s, err := a.state.LoadStatuses(ctx)
	if err != nil {
		return err
	}
	if s == nil {
		s = map[string]domain.AlertStatus{}
	}
	a.statuses = s
	return nil
}

func formatValue(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
}
