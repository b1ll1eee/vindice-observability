package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

const maxBodyBytes = 1 << 20

// --- charts --------------------------------------------------------------

type redPointDTO struct {
	T         int64   `json:"t"`
	Requests  uint64  `json:"requests"`
	Errors    uint64  `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
	latencyDTO
}

func (h *Handler) serviceSeries(w http.ResponseWriter, r *http.Request) {
	p := params{r: r}
	tr := p.timeRange()
	if p.err != nil {
		h.fail(w, p.err)
		return
	}
	step, pts, err := h.metrics.ServiceSeries(r.Context(), r.PathValue("service"), tr)
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]redPointDTO, 0, len(pts))
	for _, pt := range pts {
		out = append(out, redPointDTO{
			T: pt.Time.UnixMilli(), Requests: pt.Requests, Errors: pt.Errors,
			ErrorRate: rate(pt.Errors, pt.Requests), latencyDTO: toLatencyDTO(pt.Latency),
		})
	}
	writeJSON(w, map[string]any{"step_ms": step.Milliseconds(), "points": out})
}

func (h *Handler) metricNames(w http.ResponseWriter, r *http.Request) {
	p := params{r: r}
	tr := p.timeRange()
	if p.err != nil {
		h.fail(w, p.err)
		return
	}
	names, err := h.metrics.MetricNames(r.Context(), p.str("service"), tr)
	if err != nil {
		h.fail(w, err)
		return
	}
	type nameDTO struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Unit string `json:"unit,omitempty"`
	}
	out := make([]nameDTO, 0, len(names))
	for _, n := range names {
		out = append(out, nameDTO{Name: n.Name, Type: n.Type, Unit: n.Unit})
	}
	writeJSON(w, out)
}

// metricQuery parses ?name=&service=&agg=&group_by=&filter=key:value (repeatable).
func metricQuery(r *http.Request) (domain.MetricQuery, error) {
	v := r.URL.Query()
	q := domain.MetricQuery{
		Name: v.Get("name"), Service: v.Get("service"), Agg: domain.Aggregation(v.Get("agg")), GroupBy: v.Get("group_by"),
	}
	if q.Agg == "" {
		q.Agg = domain.AggAvg
	}
	for _, f := range v["filter"] {
		k, val, ok := strings.Cut(f, ":")
		if !ok || k == "" {
			return q, fmt.Errorf("%w: filter must be key:value", domain.ErrInvalidQuery)
		}
		if q.Filters == nil {
			q.Filters = map[string]string{}
		}
		q.Filters[k] = val
	}
	return q, nil
}

func (h *Handler) queryMetric(w http.ResponseWriter, r *http.Request) {
	p := params{r: r}
	tr := p.timeRange()
	q, err := metricQuery(r)
	if p.err != nil {
		err = p.err
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	res, err := h.metrics.QueryMetric(r.Context(), q, tr)
	if err != nil {
		h.fail(w, err)
		return
	}
	type seriesDTO struct {
		Group string `json:"group"`
		// Points are [epoch ms, value or null].
		Points [][2]any `json:"points"`
	}
	out := struct {
		StepMs    int64       `json:"step_ms"`
		Truncated bool        `json:"truncated"`
		Series    []seriesDTO `json:"series"`
	}{StepMs: res.Step.Milliseconds(), Truncated: res.Truncated, Series: []seriesDTO{}}
	for _, s := range res.Series {
		d := seriesDTO{Group: s.Group, Points: make([][2]any, 0, len(s.Points))}
		for _, pt := range s.Points {
			var v any
			if pt.Valid && !math.IsNaN(pt.Value) && !math.IsInf(pt.Value, 0) {
				v = pt.Value
			}
			d.Points = append(d.Points, [2]any{pt.Time.UnixMilli(), v})
		}
		out.Series = append(out.Series, d)
	}
	writeJSON(w, out)
}

// --- dashboards ----------------------------------------------------------

func (h *Handler) listDashboards(w http.ResponseWriter, r *http.Request) {
	ds, err := h.dashboards.ListDashboards(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, ds)
}

func (h *Handler) getDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := h.dashboards.GetDashboard(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, d)
}

func (h *Handler) saveDashboard(w http.ResponseWriter, r *http.Request) {
	var d domain.Dashboard
	if err := decode(w, r, &d); err != nil {
		h.fail(w, err)
		return
	}
	d.ID = r.PathValue("id") // empty on POST: create
	d, err := h.dashboards.SaveDashboard(r.Context(), d)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, d)
}

func (h *Handler) deleteDashboard(w http.ResponseWriter, r *http.Request) {
	if err := h.dashboards.DeleteDashboard(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- alerts --------------------------------------------------------------

// ruleDTO uses seconds for durations; the domain type uses time.Duration.
type ruleDTO struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Enabled   bool               `json:"enabled"`
	Signal    domain.SignalQuery `json:"signal"`
	WindowSec int64              `json:"window_sec"`
	Op        domain.Operator    `json:"op"`
	Threshold float64            `json:"threshold"`
	ForSec    int64              `json:"for_sec"`
	ChatID    string             `json:"chat_id,omitempty"`
	UpdatedAt time.Time          `json:"updated_at"`
	Status    *statusDTO         `json:"status,omitempty"`
}

type statusDTO struct {
	State     domain.AlertState `json:"state"`
	Since     time.Time         `json:"since"`
	Value     float64           `json:"value"`
	EvalAt    time.Time         `json:"eval_at"`
	Error     string            `json:"error,omitempty"`
	Breaching []string          `json:"breaching,omitempty"`
}

func toRuleDTO(r domain.AlertRule, st *domain.AlertStatus) ruleDTO {
	d := ruleDTO{
		ID: r.ID, Name: r.Name, Enabled: r.Enabled, Signal: r.Signal,
		WindowSec: int64(r.Window / time.Second), Op: r.Op, Threshold: r.Threshold,
		ForSec: int64(r.For / time.Second), ChatID: r.ChatID, UpdatedAt: r.UpdatedAt,
	}
	if st != nil {
		d.Status = &statusDTO{State: st.State, Since: st.Since, Value: st.Value, EvalAt: st.EvalAt, Error: st.Error, Breaching: st.Breaching}
	}
	return d
}

func (d ruleDTO) toDomain() domain.AlertRule {
	return domain.AlertRule{
		ID: d.ID, Name: d.Name, Enabled: d.Enabled, Signal: d.Signal,
		Window: time.Duration(d.WindowSec) * time.Second, Op: d.Op, Threshold: d.Threshold,
		For: time.Duration(d.ForSec) * time.Second, ChatID: d.ChatID,
	}
}

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.alerts.ListRules(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	statuses, err := h.alerts.Statuses(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]ruleDTO, 0, len(rules))
	for _, rule := range rules {
		var st *domain.AlertStatus
		if s, ok := statuses[rule.ID]; ok {
			st = &s
		}
		out = append(out, toRuleDTO(rule, st))
	}
	writeJSON(w, out)
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request) {
	rule, err := h.alerts.GetRule(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, toRuleDTO(rule, nil))
}

func (h *Handler) saveRule(w http.ResponseWriter, r *http.Request) {
	var d ruleDTO
	if err := decode(w, r, &d); err != nil {
		h.fail(w, err)
		return
	}
	d.ID = r.PathValue("id")
	rule, err := h.alerts.SaveRule(r.Context(), d.toDomain())
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, toRuleDTO(rule, nil))
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := h.alerts.DeleteRule(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) testRule(w http.ResponseWriter, r *http.Request) {
	if err := h.alerts.TestRule(r.Context(), r.PathValue("id")); err != nil {
		if !isClientError(err) {
			// Delivery failures (bad chat id, network) are useful to the user.
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) alertEvents(w http.ResponseWriter, r *http.Request) {
	p := params{r: r}
	limit := p.int("limit")
	if p.err != nil {
		h.fail(w, p.err)
		return
	}
	events, err := h.alerts.Events(r.Context(), p.str("rule_id"), limit)
	if err != nil {
		h.fail(w, err)
		return
	}
	type eventDTO struct {
		RuleID   string  `json:"rule_id"`
		RuleName string  `json:"rule_name"`
		Time     string  `json:"time"`
		State    string  `json:"state"`
		Value    float64 `json:"value"`
		Message  string  `json:"message,omitempty"`
	}
	out := make([]eventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, eventDTO{e.RuleID, e.RuleName, rfc3339(e.Time), e.State, e.Value, e.Message})
	}
	writeJSON(w, out)
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(v); err != nil {
		return fmt.Errorf("%w: invalid JSON body: %v", domain.ErrInvalidQuery, err)
	}
	return nil
}
