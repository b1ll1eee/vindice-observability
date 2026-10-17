package clickhouse

import (
	"context"
	"encoding/json"
	"time"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
	"github.com/b1ll1eee/vindice-observability/internal/core/port"
)

var (
	_ port.DashboardRepository  = (*Repository)(nil)
	_ port.AlertRuleRepository  = (*Repository)(nil)
	_ port.AlertStateRepository = (*Repository)(nil)
)

// Documents are stored as JSON in a versioned table (see 005_config.sql).
// The tables are tiny, so reading with FINAL is cheap.

func listDocs[T any](ctx context.Context, r *Repository, table string) ([]T, error) {
	rows, err := r.conn.Query(readCtx(ctx, false), "SELECT body FROM "+table+" FINAL WHERE is_deleted = 0 ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var (
			body string
			v    T
		)
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func getDoc[T any](ctx context.Context, r *Repository, table, id string) (T, bool, error) {
	var (
		v    T
		body string
	)
	rows, err := r.conn.Query(readCtx(ctx, false), "SELECT body FROM "+table+" FINAL WHERE id = ? AND is_deleted = 0", id)
	if err != nil {
		return v, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return v, false, rows.Err()
	}
	if err := rows.Scan(&body); err != nil {
		return v, false, err
	}
	return v, true, json.Unmarshal([]byte(body), &v)
}

func putDoc(ctx context.Context, r *Repository, table, id string, doc any, deleted bool) error {
	body := ""
	if !deleted {
		b, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		body = string(b)
	}
	var del uint8
	if deleted {
		del = 1
	}
	return r.conn.Exec(ctx, "INSERT INTO "+table+" (id, body, updated_at, is_deleted) VALUES (?, ?, ?, ?)",
		id, body, time.Now(), del)
}

func (r *Repository) ListDashboards(ctx context.Context) ([]domain.Dashboard, error) {
	return listDocs[domain.Dashboard](ctx, r, "dashboards")
}

func (r *Repository) GetDashboard(ctx context.Context, id string) (domain.Dashboard, bool, error) {
	return getDoc[domain.Dashboard](ctx, r, "dashboards", id)
}

func (r *Repository) SaveDashboard(ctx context.Context, d domain.Dashboard) error {
	return putDoc(ctx, r, "dashboards", d.ID, d, false)
}

func (r *Repository) DeleteDashboard(ctx context.Context, id string) error {
	return putDoc(ctx, r, "dashboards", id, nil, true)
}

func (r *Repository) ListRules(ctx context.Context) ([]domain.AlertRule, error) {
	return listDocs[domain.AlertRule](ctx, r, "alert_rules")
}

func (r *Repository) GetRule(ctx context.Context, id string) (domain.AlertRule, bool, error) {
	return getDoc[domain.AlertRule](ctx, r, "alert_rules", id)
}

func (r *Repository) SaveRule(ctx context.Context, rule domain.AlertRule) error {
	return putDoc(ctx, r, "alert_rules", rule.ID, rule, false)
}

func (r *Repository) DeleteRule(ctx context.Context, id string) error {
	return putDoc(ctx, r, "alert_rules", id, nil, true)
}

func (r *Repository) LoadStatuses(ctx context.Context) (map[string]domain.AlertStatus, error) {
	rows, err := r.conn.Query(readCtx(ctx, false), "SELECT body FROM alert_status FINAL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.AlertStatus{}
	for rows.Next() {
		var (
			body string
			s    domain.AlertStatus
		)
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			return nil, err
		}
		out[s.RuleID] = s
	}
	return out, rows.Err()
}

func (r *Repository) SaveStatuses(ctx context.Context, statuses []domain.AlertStatus) error {
	if len(statuses) == 0 {
		return nil
	}
	now := time.Now()
	return insert(ctx, r.conn, "INSERT INTO alert_status (rule_id, body, updated_at)", statuses,
		func(s *domain.AlertStatus) []any {
			b, _ := json.Marshal(s)
			return []any{s.RuleID, string(b), now}
		})
}

func (r *Repository) AppendEvents(ctx context.Context, events []domain.AlertEvent) error {
	if len(events) == 0 {
		return nil
	}
	return insert(ctx, r.conn, "INSERT INTO alert_events (rule_id, rule_name, ts, state, value, message)", events,
		func(e *domain.AlertEvent) []any {
			return []any{e.RuleID, e.RuleName, e.Time, e.State, e.Value, e.Message}
		})
}

func (r *Repository) Events(ctx context.Context, ruleID string, limit int) ([]domain.AlertEvent, error) {
	w := (&where{}).andIf(ruleID != "", "rule_id = ?", ruleID)
	rows, err := r.conn.Query(readCtx(ctx, false), `
		SELECT rule_id, rule_name, ts, state, value, message
		FROM alert_events `+w.String()+`
		ORDER BY ts DESC
		LIMIT ?`, append(w.args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AlertEvent
	for rows.Next() {
		var e domain.AlertEvent
		if err := rows.Scan(&e.RuleID, &e.RuleName, &e.Time, &e.State, &e.Value, &e.Message); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
