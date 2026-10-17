// Package httpapi is the REST API for the UI, and serves the UI itself.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

type Handler struct {
	q          port.Querier
	metrics    port.MetricsQuerier
	dashboards port.DashboardManager
	alerts     port.AlertManager
	auth       port.Authenticator
	users      port.UserManager
	keys       port.KeyManager
	cookie     cookieConfig
	log        *slog.Logger
}

type Deps struct {
	Querier    port.Querier
	Metrics    port.MetricsQuerier
	Dashboards port.DashboardManager
	Alerts     port.AlertManager
	// Auth protects every /api/v1 route; nil disables authentication
	// (only for tests).
	Auth  port.Authenticator
	Users port.UserManager
	Keys  port.KeyManager
	// SecureCookies marks the session cookie Secure (serve over HTTPS).
	SecureCookies bool
	SessionTTL    time.Duration
	// UI serves every path outside /api; may be nil.
	UI  http.Handler
	Log *slog.Logger
}

// New returns the API under /api/v1. Each route names the least role that
// may call it; route groups whose dependency is nil are not registered.
func New(d Deps) http.Handler {
	h := &Handler{
		q: d.Querier, metrics: d.Metrics, dashboards: d.Dashboards, alerts: d.Alerts,
		auth: d.Auth, users: d.Users, keys: d.Keys, log: d.Log,
		cookie: cookieConfig{secure: d.SecureCookies, ttl: d.SessionTTL},
	}
	mux := http.NewServeMux()
	route := func(pattern string, need domain.Role, fn http.HandlerFunc) {
		mux.Handle(pattern, h.guard(need, fn))
	}
	viewer, editor, admin := domain.RoleViewer, domain.RoleEditor, domain.RoleAdmin

	route("GET /api/v1/services", viewer, h.services)
	route("GET /api/v1/services/{service}/operations", viewer, h.operations)
	route("GET /api/v1/logs", viewer, h.logs)
	route("GET /api/v1/logs/histogram", viewer, h.histogram)
	route("GET /api/v1/traces", viewer, h.traces)
	route("GET /api/v1/traces/{id}", viewer, h.trace)
	if d.Metrics != nil {
		route("GET /api/v1/services/{service}/series", viewer, h.serviceSeries)
		route("GET /api/v1/metrics", viewer, h.metricNames)
		route("GET /api/v1/metrics/query", viewer, h.queryMetric)
	}
	if d.Dashboards != nil {
		route("GET /api/v1/dashboards", viewer, h.listDashboards)
		route("POST /api/v1/dashboards", editor, h.saveDashboard)
		route("GET /api/v1/dashboards/{id}", viewer, h.getDashboard)
		route("PUT /api/v1/dashboards/{id}", editor, h.saveDashboard)
		route("DELETE /api/v1/dashboards/{id}", editor, h.deleteDashboard)
	}
	if d.Alerts != nil {
		route("GET /api/v1/alerts", viewer, h.listRules)
		route("POST /api/v1/alerts", editor, h.saveRule)
		route("GET /api/v1/alerts/events", viewer, h.alertEvents)
		route("GET /api/v1/alerts/{id}", viewer, h.getRule)
		route("PUT /api/v1/alerts/{id}", editor, h.saveRule)
		route("DELETE /api/v1/alerts/{id}", editor, h.deleteRule)
		route("POST /api/v1/alerts/{id}/test", editor, h.testRule)
	}
	if d.Auth != nil {
		mux.HandleFunc("POST /api/v1/auth/login", h.login)
		route("POST /api/v1/auth/logout", viewer, h.logout)
		route("GET /api/v1/auth/me", viewer, h.me)
		route("POST /api/v1/auth/password", viewer, h.changePassword)
	}
	if d.Users != nil {
		route("GET /api/v1/users", admin, h.listUsers)
		route("POST /api/v1/users", admin, h.createUser)
		route("PUT /api/v1/users/{id}", admin, h.updateUser)
		route("DELETE /api/v1/users/{id}", admin, h.deleteUser)
	}
	if d.Keys != nil {
		route("GET /api/v1/keys", admin, h.listKeys)
		route("POST /api/v1/keys", admin, h.createKey)
		route("DELETE /api/v1/keys/{id}", admin, h.revokeKey)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})
	if d.UI != nil {
		mux.Handle("/", d.UI)
	}
	return sameOrigin(mux)
}

func (h *Handler) services(w http.ResponseWriter, r *http.Request) {
	p := params{r: r}
	tr := p.timeRange()
	if p.err != nil {
		h.fail(w, p.err)
		return
	}
	got, err := h.q.Services(r.Context(), tr)
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]serviceDTO, 0, len(got))
	for _, s := range got {
		out = append(out, serviceDTO{
			Name: s.Name, Requests: s.Requests, Errors: s.Errors, ErrorRate: rate(s.Errors, s.Requests),
			latencyDTO: toLatencyDTO(s.Latency), Logs: s.Logs, LogErrors: s.LogErrors,
		})
	}
	writeJSON(w, out)
}

func (h *Handler) operations(w http.ResponseWriter, r *http.Request) {
	p := params{r: r}
	tr := p.timeRange()
	if p.err != nil {
		h.fail(w, p.err)
		return
	}
	got, err := h.q.Operations(r.Context(), r.PathValue("service"), tr)
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]operationDTO, 0, len(got))
	for _, o := range got {
		out = append(out, operationDTO{
			Name: o.Name, Requests: o.Requests, Errors: o.Errors, ErrorRate: rate(o.Errors, o.Requests),
			latencyDTO: toLatencyDTO(o.Latency),
		})
	}
	writeJSON(w, out)
}

func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	q, err := logQuery(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	page, err := h.q.SearchLogs(r.Context(), q)
	if err != nil {
		h.fail(w, err)
		return
	}
	out := logPageDTO{Logs: toLogDTOs(page.Entries)}
	if !page.Next.IsZero() {
		out.Next = strconv.FormatInt(page.Next.UnixNano(), 10)
	}
	writeJSON(w, out)
}

func (h *Handler) histogram(w http.ResponseWriter, r *http.Request) {
	q, err := logQuery(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	hist, err := h.q.LogHistogram(r.Context(), q)
	if err != nil {
		h.fail(w, err)
		return
	}
	out := histogramDTO{StepMs: hist.Step.Milliseconds(), Buckets: make([]bucketDTO, 0, len(hist.Buckets))}
	for _, b := range hist.Buckets {
		out.Buckets = append(out.Buckets, bucketDTO{
			T: b.Time.UnixMilli(), Debug: b.Debug, Info: b.Info, Warn: b.Warn, Error: b.Errors,
		})
	}
	writeJSON(w, out)
}

func (h *Handler) traces(w http.ResponseWriter, r *http.Request) {
	p := params{r: r}
	q := domain.TraceQuery{
		Range:       p.timeRange(),
		Service:     p.str("service"),
		MinDuration: time.Duration(p.float("min_duration_ms") * float64(time.Millisecond)),
		ErrorsOnly:  p.bool("errors"),
		Limit:       p.int("limit"),
	}
	if p.err != nil {
		h.fail(w, p.err)
		return
	}
	got, err := h.q.SearchTraces(r.Context(), q)
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]traceSummaryDTO, 0, len(got))
	for _, t := range got {
		out = append(out, traceSummaryDTO{
			TraceID: idString(t.TraceID[:]), Service: t.ServiceName, Name: t.Name, Time: rfc3339(t.Timestamp),
			DurationMs: ms(time.Duration(t.DurationNs)), Status: t.StatusCode,
			HTTPMethod: t.HTTPMethod, HTTPRoute: t.HTTPRoute, HTTPStatus: t.HTTPStatus,
		})
	}
	writeJSON(w, out)
}

func (h *Handler) trace(w http.ResponseWriter, r *http.Request) {
	id, err := domain.ParseTraceID(r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	t, err := h.q.Trace(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, toTraceDTO(t))
}

func logQuery(r *http.Request) (domain.LogQuery, error) {
	p := params{r: r}
	q := domain.LogQuery{
		Range:       p.timeRange(),
		Service:     p.str("service"),
		MinSeverity: uint8(min(p.int("severity"), 255)),
		Text:        p.str("q"),
		Before:      p.nanos("before"),
		Limit:       p.int("limit"),
	}
	if s := p.str("level"); s != "" && p.err == nil {
		for _, name := range strings.Split(s, ",") {
			l, err := domain.ParseLevel(strings.TrimSpace(name))
			if err != nil {
				return q, err
			}
			q.Levels = append(q.Levels, l)
		}
	}
	if s := p.str("trace_id"); s != "" && p.err == nil {
		id, err := domain.ParseTraceID(s)
		if err != nil {
			return q, err
		}
		q.TraceID = &id
	}
	return q, p.err
}

// params reads query parameters, keeping the first error (so handlers check once).
type params struct {
	r   *http.Request
	err error
}

func (p *params) str(k string) string { return p.r.URL.Query().Get(k) }

func (p *params) parse(k string, fn func(string) error) {
	v := p.str(k)
	if v == "" || p.err != nil {
		return
	}
	if err := fn(v); err != nil {
		p.err = fmt.Errorf("%w: %s: %v", domain.ErrInvalidQuery, k, err)
	}
}

func (p *params) int(k string) (n int) {
	p.parse(k, func(v string) (err error) { n, err = strconv.Atoi(v); return })
	return n
}

func (p *params) float(k string) (f float64) {
	p.parse(k, func(v string) (err error) { f, err = strconv.ParseFloat(v, 64); return })
	return f
}

func (p *params) bool(k string) (b bool) {
	p.parse(k, func(v string) (err error) { b, err = strconv.ParseBool(v); return })
	return b
}

// millis reads epoch milliseconds.
func (p *params) millis(k string) (t time.Time) {
	p.parse(k, func(v string) error {
		n, err := strconv.ParseInt(v, 10, 64)
		t = time.UnixMilli(n)
		return err
	})
	return t
}

func (p *params) nanos(k string) (t time.Time) {
	p.parse(k, func(v string) error {
		n, err := strconv.ParseInt(v, 10, 64)
		t = time.Unix(0, n)
		return err
	})
	return t
}

func (p *params) timeRange() domain.TimeRange {
	return domain.TimeRange{From: p.millis("from"), To: p.millis("to")}
}

func isClientError(err error) bool {
	for _, e := range []error{domain.ErrInvalidQuery, domain.ErrNotFound, domain.ErrUnauthenticated,
		domain.ErrForbidden, domain.ErrBadCredentials, domain.ErrLockedOut} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidQuery):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrUnauthenticated), errors.Is(err, domain.ErrBadCredentials):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, domain.ErrLockedOut):
		writeError(w, http.StatusTooManyRequests, err.Error())
	default:
		h.log.Error("query failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query failed")
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
