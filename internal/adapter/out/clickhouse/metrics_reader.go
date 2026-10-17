package clickhouse

import (
	"context"
	"time"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

func (r *Repository) MetricNames(ctx context.Context, service string, tr domain.TimeRange) ([]domain.MetricName, error) {
	// last_seen is refreshed hourly, so look back an extra hour.
	w := (&where{}).
		and("last_seen >= fromUnixTimestamp(?)", tr.From.Add(-time.Hour).Unix()).
		andIf(service != "", "service_name = ?", service)
	rows, err := r.conn.Query(readCtx(ctx, false), `
		SELECT metric_name, toString(any(metric_type)), any(unit)
		FROM metric_series `+w.String()+`
		GROUP BY metric_name
		ORDER BY metric_name
		LIMIT 2000`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MetricName
	for rows.Next() {
		var m domain.MetricName
		if err := rows.Scan(&m.Name, &m.Type, &m.Unit); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repository) MetricSeries(ctx context.Context, name, service string, filters map[string]string, limit int) ([]domain.SeriesInfo, error) {
	w := (&where{}).
		and("metric_name = ?", name).
		andIf(service != "", "service_name = ?", service)
	for k, v := range filters {
		w.and("attributes[?] = ?", k, v)
	}
	rows, err := r.conn.Query(readCtx(ctx, false), `
		SELECT series_id, argMax(attributes, last_seen), argMax(is_monotonic, last_seen),
		       toString(argMax(temporality, last_seen))
		FROM metric_series `+w.String()+`
		GROUP BY series_id
		ORDER BY series_id
		LIMIT ?`, append(w.args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SeriesInfo
	for rows.Next() {
		var s domain.SeriesInfo
		if err := rows.Scan(&s.ID, &s.Attributes, &s.Monotonic, &s.Temporality); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) MetricBuckets(ctx context.Context, ids []uint64, tr domain.TimeRange, step time.Duration, res domain.Resolution) ([]domain.MetricBucket, error) {
	stepSec := int64(step / time.Second)
	var (
		query string
		args  []any
	)
	if res == domain.ResolutionRaw {
		query = `
			SELECT series_id, toDateTime(toStartOfInterval(timestamp, toIntervalSecond(?))) AS t,
			       min(value), max(value), sum(value), count(), argMax(value, timestamp)
			FROM metric_samples
			WHERE has(?, series_id)
			  AND timestamp >= fromUnixTimestamp64Milli(?) AND timestamp < fromUnixTimestamp64Milli(?)
			GROUP BY series_id, t`
		args = []any{stepSec, ids, tr.From.UnixMilli(), tr.To.UnixMilli()}
	} else {
		table := "metric_samples_1m"
		if res == domain.Resolution1h {
			table = "metric_samples_1h"
		}
		query = `
			SELECT series_id, toStartOfInterval(ts, toIntervalSecond(?)) AS t,
			       min(v_min), max(v_max), sum(v_sum), sum(v_count), argMaxMerge(v_last)
			FROM ` + table + `
			WHERE has(?, series_id)
			  AND ts >= fromUnixTimestamp(?) AND ts < fromUnixTimestamp(?)
			GROUP BY series_id, t`
		args = []any{stepSec, ids, tr.From.Unix(), tr.To.Unix()}
	}

	rows, err := r.conn.Query(readCtx(ctx, res != domain.ResolutionRaw), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MetricBucket
	for rows.Next() {
		var b domain.MetricBucket
		if err := rows.Scan(&b.SeriesID, &b.Time, &b.Min, &b.Max, &b.Sum, &b.Count, &b.Last); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *Repository) ServiceSeries(ctx context.Context, service string, tr domain.TimeRange, step time.Duration) ([]domain.REDPoint, error) {
	rows, err := r.conn.Query(readCtx(ctx, true), `
		SELECT toStartOfInterval(ts, toIntervalSecond(?)) AS t, sum(requests), sum(errors),
		       quantilesTDigestMerge(0.5, 0.95, 0.99)(duration_q)
		FROM red_1m
		WHERE service_name = ? AND ts >= fromUnixTimestamp(?) AND ts < fromUnixTimestamp(?)
		GROUP BY t
		ORDER BY t`, int64(step/time.Second), service, minuteFloor(tr.From), tr.To.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.REDPoint
	for rows.Next() {
		var (
			p domain.REDPoint
			q []float64
		)
		if err := rows.Scan(&p.Time, &p.Requests, &p.Errors, &q); err != nil {
			return nil, err
		}
		p.Latency = latency(q)
		out = append(out, p)
	}
	return out, rows.Err()
}
