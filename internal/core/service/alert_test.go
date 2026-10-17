package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

type memRules struct{ rules map[string]domain.AlertRule }

func (m *memRules) ListRules(context.Context) ([]domain.AlertRule, error) {
	var out []domain.AlertRule
	for _, r := range m.rules {
		out = append(out, r)
	}
	return out, nil
}
func (m *memRules) GetRule(_ context.Context, id string) (domain.AlertRule, bool, error) {
	r, ok := m.rules[id]
	return r, ok, nil
}
func (m *memRules) SaveRule(_ context.Context, r domain.AlertRule) error {
	m.rules[r.ID] = r
	return nil
}
func (m *memRules) DeleteRule(_ context.Context, id string) error { delete(m.rules, id); return nil }

type memState struct {
	saved  map[string]domain.AlertStatus
	events []domain.AlertEvent
}

func (m *memState) LoadStatuses(context.Context) (map[string]domain.AlertStatus, error) {
	out := map[string]domain.AlertStatus{}
	for k, v := range m.saved {
		out[k] = v
	}
	return out, nil
}
func (m *memState) SaveStatuses(_ context.Context, s []domain.AlertStatus) error {
	for _, st := range s {
		m.saved[st.RuleID] = st
	}
	return nil
}
func (m *memState) AppendEvents(_ context.Context, e []domain.AlertEvent) error {
	m.events = append(m.events, e...)
	return nil
}
func (m *memState) Events(context.Context, string, int) ([]domain.AlertEvent, error) {
	return m.events, nil
}

type fakeSignals struct {
	vals []GroupValue
	err  error
}

func (f *fakeSignals) SignalValues(context.Context, domain.SignalQuery, domain.TimeRange) ([]GroupValue, error) {
	return f.vals, f.err
}

type fakeNotifier struct {
	sent []domain.Notification
	err  error
}

func (f *fakeNotifier) Notify(_ context.Context, n domain.Notification) error {
	f.sent = append(f.sent, n)
	return f.err
}

func newAlerts(rule domain.AlertRule) (*AlertService, *fakeSignals, *fakeNotifier, *memState, *time.Time) {
	sig, notif := &fakeSignals{}, &fakeNotifier{}
	state := &memState{saved: map[string]domain.AlertStatus{}}
	a := NewAlertService(AlertDeps{
		Rules:     &memRules{rules: map[string]domain.AlertRule{rule.ID: rule}},
		State:     state,
		Signals:   sig,
		Notifier:  notif,
		PublicURL: "http://vindice:8080/",
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	now := t0
	a.now = func() time.Time { return now }
	a.evaluating = true // as if Run were driving it
	return a, sig, notif, state, &now
}

var errorRateRule = domain.AlertRule{
	ID: "r1", Name: "orders errors", Enabled: true,
	Signal: domain.SignalQuery{Kind: domain.SignalKindRED, Service: "orders", Measure: domain.MeasureErrorRate},
	Window: 5 * time.Minute, Op: domain.OpGT, Threshold: 0.05, For: 2 * time.Minute,
}

func TestAlertLifecycle(t *testing.T) {
	a, sig, notif, state, now := newAlerts(errorRateRule)
	ctx := context.Background()
	step := func(v float64, d time.Duration) domain.AlertState {
		t.Helper()
		*now = now.Add(d)
		sig.vals = []GroupValue{{Value: v}}
		if err := a.Evaluate(ctx); err != nil {
			t.Fatal(err)
		}
		st, _ := a.Statuses(ctx)
		return st["r1"].State
	}

	if s := step(0.01, 0); s != domain.AlertOK {
		t.Fatalf("healthy: %s", s)
	}
	if s := step(0.2, time.Minute); s != domain.AlertPending {
		t.Fatalf("first breach should be pending (for=2m): %s", s)
	}
	if s := step(0.2, time.Minute); s != domain.AlertPending || len(notif.sent) != 0 {
		t.Fatalf("1m into breach: %s, %d sent", s, len(notif.sent))
	}
	if s := step(0.3, time.Minute); s != domain.AlertFiring || len(notif.sent) != 1 {
		t.Fatalf("2m into breach: %s, %d sent", s, len(notif.sent))
	}
	if n := notif.sent[0]; n.Event != domain.EventFiring || n.Value != 0.3 || n.URL != "http://vindice:8080/alerts" {
		t.Errorf("firing notification = %+v", n)
	}
	if s := step(0.3, time.Minute); s != domain.AlertFiring || len(notif.sent) != 1 {
		t.Fatalf("still firing must not re-notify: %s, %d sent", s, len(notif.sent))
	}
	if s := step(0.0, time.Minute); s != domain.AlertOK || len(notif.sent) != 2 || notif.sent[1].Event != domain.EventResolved {
		t.Fatalf("recovery: %s, %+v", s, notif.sent)
	}
	if len(state.events) != 2 || state.events[0].State != domain.EventFiring || state.events[1].State != domain.EventResolved {
		t.Errorf("events = %+v", state.events)
	}
}

func TestAlertPendingResetsOnRecovery(t *testing.T) {
	a, sig, notif, _, now := newAlerts(errorRateRule)
	ctx := context.Background()
	sig.vals = []GroupValue{{Value: 0.5}}
	_ = a.Evaluate(ctx)
	*now = now.Add(time.Minute)
	sig.vals = []GroupValue{{Value: 0}}
	_ = a.Evaluate(ctx)
	*now = now.Add(time.Minute)
	sig.vals = []GroupValue{{Value: 0.5}}
	_ = a.Evaluate(ctx)
	st, _ := a.Statuses(ctx)
	if st["r1"].State != domain.AlertPending || len(notif.sent) != 0 {
		t.Errorf("a dip below threshold must restart the hold: %s, %d sent", st["r1"].State, len(notif.sent))
	}
}

func TestAlertQueryErrorKeepsState(t *testing.T) {
	rule := errorRateRule
	rule.For = 0
	a, sig, notif, _, now := newAlerts(rule)
	ctx := context.Background()
	sig.vals = []GroupValue{{Value: 0.5}}
	_ = a.Evaluate(ctx)
	*now = now.Add(time.Minute)
	sig.err = errors.New("clickhouse down")
	_ = a.Evaluate(ctx)
	st, _ := a.Statuses(ctx)
	if st["r1"].State != domain.AlertFiring || st["r1"].Error == "" || len(notif.sent) != 1 {
		t.Errorf("query error must not resolve: %+v, %d sent", st["r1"], len(notif.sent))
	}
}

func TestAlertGroupsAndWorstValue(t *testing.T) {
	rule := errorRateRule
	rule.For, rule.Op, rule.Threshold = 0, domain.OpLT, 10
	a, sig, notif, _, _ := newAlerts(rule)
	sig.vals = []GroupValue{{Group: "a", Value: 50}, {Group: "b", Value: 3}, {Group: "c", Value: 7}}
	if err := a.Evaluate(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := notif.sent[0]
	if n.Value != 3 || len(n.Breaching) != 2 || n.Breaching[0] != "b = 3" {
		t.Errorf("notification = %+v", n)
	}
}

func TestAlertStateSurvivesRestart(t *testing.T) {
	rule := errorRateRule
	rule.For = 0
	a, sig, notif, state, _ := newAlerts(rule)
	sig.vals = []GroupValue{{Value: 0.5}}
	_ = a.Evaluate(context.Background())

	// A new service over the same state store must not fire again.
	b := NewAlertService(AlertDeps{
		Rules: &memRules{rules: map[string]domain.AlertRule{rule.ID: rule}}, State: state,
		Signals: sig, Notifier: notif, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	_ = b.Evaluate(context.Background())
	if len(notif.sent) != 1 {
		t.Errorf("restart re-sent the alert: %d notifications", len(notif.sent))
	}
}

func TestRuleValidation(t *testing.T) {
	bad := []domain.AlertRule{
		{Name: "", Window: time.Minute, Op: domain.OpGT, Signal: errorRateRule.Signal},
		{Name: "x", Window: time.Second, Op: domain.OpGT, Signal: errorRateRule.Signal},
		{Name: "x", Window: time.Minute, Op: "!=", Signal: errorRateRule.Signal},
		{Name: "x", Window: time.Minute, Op: domain.OpGT, Signal: domain.SignalQuery{Kind: domain.SignalKindRED, Service: "a", Measure: "p42"}},
		{Name: "x", Window: time.Minute, Op: domain.OpGT, Signal: domain.SignalQuery{Kind: domain.SignalKindMetric}},
	}
	for i, r := range bad {
		if err := r.Validate(); !errors.Is(err, domain.ErrInvalidQuery) {
			t.Errorf("rule %d accepted: %v", i, err)
		}
	}
}
