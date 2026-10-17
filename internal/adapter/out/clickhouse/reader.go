package clickhouse

import (
	"context"
	"encoding/hex"
	"strings"
	"time"
	"unicode"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

var _ port.TelemetryReader = (*Repository)(nil)

// Read queries run with a time limit. Queries over rollups also use the
// ClickHouse query cache: they carry explicit time bounds (no now()), so a
// cached result is exact for its inputs, and dashboards repeat them often.
func readCtx(ctx context.Context, cached bool) context.Context {
	s := clickhouse.Settings{"max_execution_time": 30}
	if cached {
		s["use_query_cache"] = 1
		s["query_cache_ttl"] = 30
	}
	return clickhouse.Context(ctx, clickhouse.WithSettings(s))
}

// where builds a WHERE clause from conditions and their bound arguments
// (Builder pattern). Values are always bound, never formatted into SQL.
type where struct {
	conds []string
	args  []any
}

func (w *where) and(cond string, args ...any) *where {
	w.conds = append(w.conds, cond)
	w.args = append(w.args, args...)
	return w
}

func (w *where) andIf(ok bool, cond string, args ...any) *where {
	if ok {
		w.and(cond, args...)
	}
	return w
}

func (w *where) String() string {
	if len(w.conds) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(w.conds, " AND ")
}

func nanos(t time.Time) int64 { return t.UnixNano() }

// minuteFloor aligns to the per-minute rollups.
func minuteFloor(t time.Time) int64 { return t.Truncate(time.Minute).Unix() }

func hexID(b []byte) string { return hex.EncodeToString(b) }

// tokens splits text into the tokens tokenbf indexes: runs of ASCII letters
// and digits, or of non-ASCII characters (e.g. Thai).
func tokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r < 0x80 && !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func (r *Repository) ServiceRED(ctx context.Context, tr domain.TimeRange) ([]domain.ServiceSummary, error) {
	rows, err := r.conn.Query(readCtx(ctx, true), `
		SELECT service_name, sum(requests), sum(errors),
		       quantilesTDigestMerge(0.5, 0.95, 0.99)(duration_q)
		FROM red_1m
		WHERE ts >= fromUnixTimestamp(?) AND ts < fromUnixTimestamp(?)
		GROUP BY service_name`, minuteFloor(tr.From), tr.To.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ServiceSummary
	for rows.Next() {
		var (
			s domain.ServiceSummary
			q []float64
		)
		if err := rows.Scan(&s.Name, &s.Requests, &s.Errors, &q); err != nil {
			return nil, err
		}
		s.Latency = latency(q)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) ServiceLogCounts(ctx context.Context, tr domain.TimeRange) (map[string]domain.LogCount, error) {
	rows, err := r.conn.Query(readCtx(ctx, true), `
		SELECT service_name, sum(records), sumIf(records, severity_number >= 17)
		FROM log_counts_1m
		WHERE ts >= fromUnixTimestamp(?) AND ts < fromUnixTimestamp(?)
		GROUP BY service_name`, minuteFloor(tr.From), tr.To.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.LogCount{}
	for rows.Next() {
		var (
			name string
			c    domain.LogCount
		)
		if err := rows.Scan(&name, &c.Total, &c.Errors); err != nil {
			return nil, err
		}
		out[name] = c
	}
	return out, rows.Err()
}

func (r *Repository) OperationRED(ctx context.Context, service string, tr domain.TimeRange) ([]domain.OperationSummary, error) {
	rows, err := r.conn.Query(readCtx(ctx, true), `
		SELECT span_name, sum(requests) AS n, sum(errors),
		       quantilesTDigestMerge(0.5, 0.95, 0.99)(duration_q)
		FROM red_1m
		WHERE service_name = ? AND ts >= fromUnixTimestamp(?) AND ts < fromUnixTimestamp(?)
		GROUP BY span_name
		ORDER BY n DESC`, service, minuteFloor(tr.From), tr.To.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OperationSummary
	for rows.Next() {
		var (
			o domain.OperationSummary
			q []float64
		)
		if err := rows.Scan(&o.Name, &o.Requests, &o.Errors, &q); err != nil {
			return nil, err
		}
		o.Latency = latency(q)
		out = append(out, o)
	}
	return out, rows.Err()
}

// latency converts nanosecond quantiles to milliseconds.
func latency(q []float64) domain.Latency {
	if len(q) != 3 {
		return domain.Latency{}
	}
	return domain.Latency{P50: q[0] / 1e6, P95: q[1] / 1e6, P99: q[2] / 1e6}
}

// minSeverity keeps logs at or above a severity. Logs without a severity
// (0) count as INFO here, as they do in retention and the level histogram.
const minSeverity = "(severity_number >= ? OR (severity_number = 0 AND ? <= 9))"

// levelRanges are the severity numbers of each level, matching
// domain.LevelOf (no severity counts as INFO).
var levelRanges = map[domain.SeverityLevel]string{
	domain.LevelDebug: "severity_number BETWEEN 1 AND 8",
	domain.LevelInfo:  "(severity_number = 0 OR severity_number BETWEEN 9 AND 12)",
	domain.LevelWarn:  "severity_number BETWEEN 13 AND 16",
	domain.LevelError: "severity_number >= 17",
}

// levelsCond ORs the ranges of the chosen levels. The SQL comes only from
// levelRanges, never from input.
func levelsCond(levels []domain.SeverityLevel) string {
	parts := make([]string, 0, len(levels))
	for _, l := range levels {
		if r, ok := levelRanges[l]; ok {
			parts = append(parts, r)
		}
	}
	if len(parts) == 0 {
		return "0" // only unknown levels: match nothing
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func logFilter(q domain.LogQuery) *where {
	w := (&where{}).
		and("timestamp >= fromUnixTimestamp64Nano(?)", nanos(q.Range.From)).
		and("timestamp < fromUnixTimestamp64Nano(?)", nanos(q.Range.To)).
		andIf(q.Service != "", "service_name = ?", q.Service).
		andIf(q.MinSeverity > 0, minSeverity, q.MinSeverity, q.MinSeverity).
		andIf(len(q.Levels) > 0, levelsCond(q.Levels)).
		andIf(!q.Before.IsZero(), "timestamp < fromUnixTimestamp64Nano(?)", nanos(q.Before))
	if q.TraceID != nil {
		w.and("trace_id = unhex(?)", hexID(q.TraceID[:]))
	}
	if q.Text != "" {
		// hasToken uses the token index to skip granules; the substring
		// check then makes the match exact (phrase, case-insensitive).
		for _, t := range tokens(q.Text) {
			w.and("hasToken(lower(body), ?)", t)
		}
		w.and("positionCaseInsensitiveUTF8(body, ?) > 0", q.Text)
	}
	return w
}

func (r *Repository) SearchLogs(ctx context.Context, q domain.LogQuery) ([]domain.LogEntry, error) {
	w := logFilter(q)
	entries, ids, err := r.queryLogs(ctx, w.String()+" ORDER BY timestamp DESC LIMIT ?", append(w.args, q.Limit)...)
	if err != nil {
		return nil, err
	}
	return entries, r.attachResources(ctx, entries, ids)
}

func (r *Repository) TraceLogs(ctx context.Context, id [16]byte, tr domain.TimeRange) ([]domain.LogEntry, error) {
	w := logFilter(domain.LogQuery{Range: tr, TraceID: &id})
	entries, ids, err := r.queryLogs(ctx, w.String()+" ORDER BY timestamp LIMIT 1000", w.args...)
	if err != nil {
		return nil, err
	}
	return entries, r.attachResources(ctx, entries, ids)
}

func (r *Repository) queryLogs(ctx context.Context, tail string, args ...any) ([]domain.LogEntry, []uint64, error) {
	rows, err := r.conn.Query(readCtx(ctx, false), `
		SELECT timestamp, service_name, severity_number, severity_text, trace_id, span_id,
		       body, attributes, resource_id
		FROM logs `+tail, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var (
		out []domain.LogEntry
		ids []uint64
	)
	for rows.Next() {
		var (
			e             domain.LogEntry
			traceID, span string
			resID         uint64
		)
		if err := rows.Scan(&e.Timestamp, &e.ServiceName, &e.SeverityNumber, &e.SeverityText, &traceID, &span,
			&e.Body, &e.Attributes, &resID); err != nil {
			return nil, nil, err
		}
		copy(e.TraceID[:], traceID)
		copy(e.SpanID[:], span)
		out = append(out, e)
		ids = append(ids, resID)
	}
	return out, ids, rows.Err()
}

// attachResources looks resources up separately instead of joining, so the
// join never runs over rows that the LIMIT would discard.
func (r *Repository) attachResources(ctx context.Context, entries []domain.LogEntry, ids []uint64) error {
	res, err := r.Resources(ctx, ids)
	if err != nil {
		return err
	}
	for i := range entries {
		entries[i].Resource = res[ids[i]]
	}
	return nil
}

func (r *Repository) Resources(ctx context.Context, ids []uint64) (map[uint64]map[string]string, error) {
	out := map[uint64]map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.conn.Query(readCtx(ctx, false), `
		SELECT resource_id, argMax(attributes, last_seen)
		FROM resources
		WHERE has(?, resource_id)
		GROUP BY resource_id`, uniq(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id    uint64
			attrs map[string]string
		)
		if err := rows.Scan(&id, &attrs); err != nil {
			return nil, err
		}
		out[id] = attrs
	}
	return out, rows.Err()
}

func uniq(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

const levelCounts = `
	sumIf(n, severity_number BETWEEN 1 AND 8),
	sumIf(n, severity_number = 0 OR severity_number BETWEEN 9 AND 12),
	sumIf(n, severity_number BETWEEN 13 AND 16),
	sumIf(n, severity_number >= 17)`

func (r *Repository) LogHistogram(ctx context.Context, q domain.LogQuery, step time.Duration, rollup bool) ([]domain.HistogramBucket, error) {
	var (
		query string
		args  []any
	)
	stepSec := int64(step / time.Second)
	if rollup {
		w := (&where{}).
			and("ts >= fromUnixTimestamp(?)", minuteFloor(q.Range.From)).
			and("ts < fromUnixTimestamp(?)", q.Range.To.Unix()).
			andIf(q.Service != "", "service_name = ?", q.Service).
			andIf(q.MinSeverity > 0, minSeverity, q.MinSeverity, q.MinSeverity).
			andIf(len(q.Levels) > 0, levelsCond(q.Levels))
		query = `SELECT toStartOfInterval(ts, toIntervalSecond(?)) AS t,` + levelCounts + `
			FROM (SELECT ts, severity_number, records AS n FROM log_counts_1m ` + w.String() + `)
			GROUP BY t ORDER BY t`
		args = append([]any{stepSec}, w.args...)
	} else {
		w := logFilter(q)
		query = `SELECT toDateTime(toStartOfInterval(timestamp, toIntervalSecond(?))) AS t,` + levelCounts + `
			FROM (SELECT timestamp, severity_number, 1 AS n FROM logs ` + w.String() + `)
			GROUP BY t ORDER BY t`
		args = append([]any{stepSec}, w.args...)
	}

	rows, err := r.conn.Query(readCtx(ctx, rollup), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HistogramBucket
	for rows.Next() {
		var b domain.HistogramBucket
		if err := rows.Scan(&b.Time, &b.Debug, &b.Info, &b.Warn, &b.Errors); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *Repository) SearchTraces(ctx context.Context, q domain.TraceQuery) ([]domain.TraceSummary, error) {
	w := (&where{}).
		and("timestamp >= fromUnixTimestamp64Nano(?)", nanos(q.Range.From)).
		and("timestamp < fromUnixTimestamp64Nano(?)", nanos(q.Range.To)).
		and("parent_span_id = toFixedString('', 8)").
		andIf(q.Service != "", "service_name = ?", q.Service).
		andIf(q.MinDuration > 0, "duration_ns >= ?", uint64(q.MinDuration))
	if q.ErrorsOnly {
		w.and(`trace_id IN (
			SELECT trace_id FROM spans
			WHERE status_code = 'Error'
			  AND timestamp >= fromUnixTimestamp64Nano(?) AND timestamp < fromUnixTimestamp64Nano(?))`,
			nanos(q.Range.From), nanos(q.Range.To))
	}
	rows, err := r.conn.Query(readCtx(ctx, false), `
		SELECT trace_id, service_name, span_name, timestamp, duration_ns, status_code,
		       http_method, http_route, http_status_code
		FROM spans `+w.String()+`
		ORDER BY timestamp DESC
		LIMIT ?`, append(w.args, q.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TraceSummary
	for rows.Next() {
		var (
			t       domain.TraceSummary
			traceID string
		)
		if err := rows.Scan(&traceID, &t.ServiceName, &t.Name, &t.Timestamp, &t.DurationNs, &t.StatusCode,
			&t.HTTPMethod, &t.HTTPRoute, &t.HTTPStatus); err != nil {
			return nil, err
		}
		copy(t.TraceID[:], traceID)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) TraceWindow(ctx context.Context, id [16]byte) (domain.TimeRange, bool, error) {
	var (
		tr domain.TimeRange
		n  uint64
	)
	err := r.conn.QueryRow(readCtx(ctx, false), `
		SELECT min(start_ts), max(end_ts), count()
		FROM trace_index
		WHERE trace_id = unhex(?)`, hexID(id[:])).Scan(&tr.From, &tr.To, &n)
	return tr, n > 0, err
}

func (r *Repository) TraceSpans(ctx context.Context, id [16]byte, tr domain.TimeRange) ([]domain.SpanRow, error) {
	rows, err := r.conn.Query(readCtx(ctx, false), `
		SELECT timestamp, span_id, parent_span_id, service_name, span_name, span_kind, status_code,
		       status_message, duration_ns, resource_id, scope_name, http_method, http_route,
		       http_status_code, attributes, events_timestamp, events_name, events_attributes
		FROM spans
		WHERE trace_id = unhex(?)
		  AND timestamp >= fromUnixTimestamp64Nano(?) AND timestamp <= fromUnixTimestamp64Nano(?)
		ORDER BY timestamp
		LIMIT 10000`, hexID(id[:]), nanos(tr.From), nanos(tr.To))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SpanRow
	for rows.Next() {
		var (
			s            domain.SpanRow
			span, parent string
		)
		if err := rows.Scan(&s.Timestamp, &span, &parent, &s.ServiceName, &s.Name, &s.Kind, &s.StatusCode,
			&s.StatusMessage, &s.DurationNs, &s.ResourceID, &s.ScopeName, &s.HTTPMethod, &s.HTTPRoute,
			&s.HTTPStatus, &s.Attributes, &s.EventTimes, &s.EventNames, &s.EventAttrs); err != nil {
			return nil, err
		}
		s.TraceID = id
		copy(s.SpanID[:], span)
		copy(s.ParentSpanID[:], parent)
		out = append(out, s)
	}
	return out, rows.Err()
}
