package service

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
	"github.com/b1ll1eee/vindice-observability/internal/core/port"
)

const (
	maxSeriesPerQuery = 500
	maxGroups         = 50
)

var _ port.MetricsQuerier = (*QueryService)(nil)

func (s *QueryService) MetricNames(ctx context.Context, service string, r domain.TimeRange) ([]domain.MetricName, error) {
	r, err := s.normalize(r)
	if err != nil {
		return nil, err
	}
	return s.reader.MetricNames(ctx, service, r)
}

func (s *QueryService) QueryMetric(ctx context.Context, q domain.MetricQuery, r domain.TimeRange) (domain.MetricResult, error) {
	if err := q.Validate(); err != nil {
		return domain.MetricResult{}, err
	}
	r, err := s.normalize(r)
	if err != nil {
		return domain.MetricResult{}, err
	}
	return s.queryMetric(ctx, q, r, pickStep(r.To.Sub(r.From)))
}

func (s *QueryService) ServiceSeries(ctx context.Context, service string, r domain.TimeRange) (time.Duration, []domain.REDPoint, error) {
	r, err := s.normalize(r)
	if err != nil {
		return 0, nil, err
	}
	step := max(pickStep(r.To.Sub(r.From)), time.Minute) // red_1m is per minute
	got, err := s.reader.ServiceSeries(ctx, service, r, step)
	if err != nil {
		return 0, nil, err
	}
	byTime := make(map[int64]domain.REDPoint, len(got))
	for _, p := range got {
		byTime[p.Time.Unix()] = p
	}
	var out []domain.REDPoint
	for t := r.From.Truncate(step); t.Before(r.To); t = t.Add(step) {
		p, ok := byTime[t.Unix()]
		if !ok {
			p = domain.REDPoint{Time: t}
		}
		out = append(out, p)
	}
	return step, out, nil
}

// resolutionFor picks the coarsest table that still has one row per step,
// and rounds the step up to that table's granularity.
func resolutionFor(step time.Duration) (domain.Resolution, time.Duration) {
	switch {
	case step >= time.Hour:
		return domain.Resolution1h, step.Round(time.Hour)
	case step >= time.Minute:
		return domain.Resolution1m, step.Round(time.Minute)
	}
	return domain.ResolutionRaw, step
}

func (s *QueryService) queryMetric(ctx context.Context, q domain.MetricQuery, r domain.TimeRange, step time.Duration) (domain.MetricResult, error) {
	res, step := resolutionFor(step)
	out := domain.MetricResult{Step: step}

	series, err := s.reader.MetricSeries(ctx, q.Name, q.Service, q.Filters, maxSeriesPerQuery+1)
	if err != nil || len(series) == 0 {
		return out, err
	}
	if len(series) > maxSeriesPerQuery {
		series, out.Truncated = series[:maxSeriesPerQuery], true
	}
	ids := make([]uint64, len(series))
	for i, si := range series {
		ids[i] = si.ID
	}

	start := r.From.Truncate(step)
	from := start
	if q.Agg == domain.AggRate {
		from = from.Add(-step) // the bucket before the range gives the first delta
	}
	buckets, err := s.reader.MetricBuckets(ctx, ids, domain.TimeRange{From: from, To: r.To}, step, res)
	if err != nil {
		return out, err
	}

	bySeries := map[uint64]map[int64]domain.MetricBucket{}
	for _, b := range buckets {
		m := bySeries[b.SeriesID]
		if m == nil {
			m = map[int64]domain.MetricBucket{}
			bySeries[b.SeriesID] = m
		}
		m[b.Time.Unix()] = b
	}

	var times []time.Time
	for t := start; t.Before(r.To); t = t.Add(step) {
		times = append(times, t)
	}

	// Per series values, then merged into groups.
	groups := map[string][][]domain.MetricPoint{}
	for _, si := range series {
		pts := make([]domain.MetricPoint, len(times))
		for i, t := range times {
			pts[i] = seriesValue(q.Agg, si, bySeries[si.ID], t, step)
		}
		g := ""
		if q.GroupBy != "" {
			g = si.Attributes[q.GroupBy]
		}
		groups[g] = append(groups[g], pts)
	}

	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	slices.SortFunc(names, strings.Compare)
	if len(names) > maxGroups {
		names, out.Truncated = names[:maxGroups], true
	}
	for _, g := range names {
		out.Series = append(out.Series, domain.MetricSeries{Group: g, Points: combine(q.Agg, times, groups[g])})
	}
	return out, nil
}

// seriesValue reduces one series' bucket at t to a value.
func seriesValue(agg domain.Aggregation, si domain.SeriesInfo, b map[int64]domain.MetricBucket, t time.Time, step time.Duration) domain.MetricPoint {
	p := domain.MetricPoint{Time: t}
	cur, ok := b[t.Unix()]
	if !ok || cur.Count == 0 {
		return p
	}
	p.Valid = true
	switch agg {
	case domain.AggAvg:
		p.Value = cur.Sum / float64(cur.Count)
	case domain.AggSum:
		p.Value = cur.Sum
	case domain.AggMin:
		p.Value = cur.Min
	case domain.AggMax:
		p.Value = cur.Max
	case domain.AggCount:
		p.Value = float64(cur.Count)
	case domain.AggLast:
		p.Value = cur.Last
	case domain.AggRate:
		secs := step.Seconds()
		if si.Temporality == "delta" {
			p.Value = cur.Sum / secs
			break
		}
		prev, ok := b[t.Add(-step).Unix()]
		if !ok || prev.Count == 0 {
			p.Valid = false
			break
		}
		d := cur.Last - prev.Last
		if d < 0 { // counter reset: count from zero
			d = cur.Last
		}
		p.Value = d / secs
	}
	return p
}

// combine merges the series of one group point by point: averages stay
// averages, min/max stay min/max, everything else adds up.
func combine(agg domain.Aggregation, times []time.Time, series [][]domain.MetricPoint) []domain.MetricPoint {
	out := make([]domain.MetricPoint, len(times))
	for i, t := range times {
		var (
			n   int
			acc float64
		)
		for _, s := range series {
			p := s[i]
			if !p.Valid {
				continue
			}
			switch {
			case n == 0:
				acc = p.Value
			case agg == domain.AggMin:
				acc = math.Min(acc, p.Value)
			case agg == domain.AggMax:
				acc = math.Max(acc, p.Value)
			default:
				acc += p.Value
			}
			n++
		}
		if agg == domain.AggAvg && n > 0 {
			acc /= float64(n)
		}
		out[i] = domain.MetricPoint{Time: t, Value: acc, Valid: n > 0}
	}
	return out
}

// GroupValue is a signal reduced to one number for an alert.
type GroupValue struct {
	Group string
	Value float64
}

// SignalValues reduces a signal over r to one value per group. Alert rules
// use it; dashboards chart the same Signal over time instead.
func (s *QueryService) SignalValues(ctx context.Context, sig domain.SignalQuery, r domain.TimeRange) ([]GroupValue, error) {
	switch sig.Kind {
	case domain.SignalKindMetric:
		res, err := s.queryMetric(ctx, *sig.Metric, r, time.Minute)
		if err != nil {
			return nil, err
		}
		var out []GroupValue
		for _, ser := range res.Series {
			if v, ok := reduce(sig.Metric.Agg, ser.Points); ok {
				out = append(out, GroupValue{Group: ser.Group, Value: v})
			}
		}
		return out, nil

	case domain.SignalKindRED:
		red, err := s.reader.ServiceRED(ctx, r)
		if err != nil {
			return nil, err
		}
		for _, svc := range red {
			if svc.Name == sig.Service {
				return []GroupValue{{Value: domain.REDValue(sig.Measure, svc.Requests, svc.Errors, svc.Latency)}}, nil
			}
		}
		// No requests at all is a value too (alerts on missing traffic).
		return []GroupValue{{Value: domain.REDValue(sig.Measure, 0, 0, domain.Latency{})}}, nil

	case domain.SignalKindLogs:
		h, err := s.LogHistogram(ctx, domain.LogQuery{
			Range: r, Service: sig.Service, MinSeverity: sig.MinSeverity, Text: sig.Text,
		})
		if err != nil {
			return nil, err
		}
		var n uint64
		for _, b := range h.Buckets {
			n += b.Debug + b.Info + b.Warn + b.Errors
		}
		return []GroupValue{{Value: float64(n)}}, nil
	}
	return nil, domain.ErrInvalidQuery
}

// reduce folds a window's points into one value with the same aggregation.
func reduce(agg domain.Aggregation, pts []domain.MetricPoint) (float64, bool) {
	var (
		n         int
		acc, last float64
	)
	for _, p := range pts {
		if !p.Valid {
			continue
		}
		switch {
		case n == 0:
			acc = p.Value
		case agg == domain.AggMin:
			acc = math.Min(acc, p.Value)
		case agg == domain.AggMax:
			acc = math.Max(acc, p.Value)
		default:
			acc += p.Value
		}
		last = p.Value
		n++
	}
	switch {
	case n == 0:
		return 0, false
	case agg == domain.AggAvg || agg == domain.AggRate:
		return acc / float64(n), true
	case agg == domain.AggLast:
		return last, true
	}
	return acc, true
}
