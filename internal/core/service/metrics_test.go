package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

func bucket(id uint64, t time.Time, last float64) domain.MetricBucket {
	return domain.MetricBucket{SeriesID: id, Time: t, Min: last, Max: last, Sum: last, Count: 1, Last: last}
}

func TestQueryMetricRateAndGroupBy(t *testing.T) {
	step := time.Minute
	start := t0.Add(-3 * step)
	r := &fakeReader{
		series: []domain.SeriesInfo{
			{ID: 1, Attributes: map[string]string{"route": "/a"}, Temporality: "cumulative"},
			{ID: 2, Attributes: map[string]string{"route": "/a"}, Temporality: "cumulative"},
			{ID: 3, Attributes: map[string]string{"route": "/b"}, Temporality: "cumulative"},
		},
		metricBuckets: []domain.MetricBucket{
			// the bucket before the range, needed for the first rate
			bucket(1, start.Add(-step), 0), bucket(1, start, 60), bucket(1, start.Add(step), 120), bucket(1, start.Add(2*step), 30), // reset
			bucket(2, start.Add(-step), 0), bucket(2, start, 120),
			bucket(3, start, 600),
		},
	}
	s := newQuery(r)
	res, err := s.queryMetric(context.Background(),
		domain.MetricQuery{Name: "http_requests", Agg: domain.AggRate, GroupBy: "route"},
		domain.TimeRange{From: start, To: t0}, step)
	if err != nil {
		t.Fatal(err)
	}
	if r.gotRes != domain.Resolution1m || !r.gotBucketRange.From.Equal(start.Add(-step)) {
		t.Errorf("resolution %v, from %v", r.gotRes, r.gotBucketRange.From)
	}
	if len(res.Series) != 2 || res.Series[0].Group != "/a" || res.Series[1].Group != "/b" {
		t.Fatalf("groups = %+v", res.Series)
	}
	a := res.Series[0].Points
	// /a: series 1 (+60) and 2 (+120) per minute -> 1/s + 2/s = 3/s
	if !a[0].Valid || a[0].Value != 3 {
		t.Errorf("/a first point = %+v", a[0])
	}
	// next minute only series 1: +60 -> 1/s
	if a[1].Value != 1 {
		t.Errorf("/a second point = %+v", a[1])
	}
	// counter reset 120 -> 30 counts 30 from zero -> 0.5/s
	if a[2].Value != 0.5 {
		t.Errorf("/a reset point = %+v", a[2])
	}
	// /b has no earlier bucket, so no rate can be computed
	if res.Series[1].Points[0].Valid {
		t.Errorf("/b without a previous bucket should be empty: %+v", res.Series[1].Points[0])
	}
}

func TestCombineAndReduce(t *testing.T) {
	times := []time.Time{t0}
	pts := func(v ...float64) [][]domain.MetricPoint {
		var out [][]domain.MetricPoint
		for _, x := range v {
			out = append(out, []domain.MetricPoint{{Time: t0, Value: x, Valid: true}})
		}
		return out
	}
	for agg, want := range map[domain.Aggregation]float64{
		domain.AggAvg: 2, domain.AggSum: 6, domain.AggMin: 1, domain.AggMax: 3,
	} {
		if got := combine(agg, times, pts(1, 2, 3))[0].Value; got != want {
			t.Errorf("combine %s = %v, want %v", agg, got, want)
		}
	}

	window := []domain.MetricPoint{{Value: 4, Valid: true}, {Valid: false}, {Value: 2, Valid: true}}
	for agg, want := range map[domain.Aggregation]float64{
		domain.AggAvg: 3, domain.AggRate: 3, domain.AggSum: 6, domain.AggMax: 4, domain.AggMin: 2, domain.AggLast: 2,
	} {
		if got, ok := reduce(agg, window); !ok || got != want {
			t.Errorf("reduce %s = %v, want %v", agg, got, want)
		}
	}
	if _, ok := reduce(domain.AggAvg, []domain.MetricPoint{{}}); ok {
		t.Error("reduce of no data should report no value")
	}
}

func TestResolutionFor(t *testing.T) {
	for step, want := range map[time.Duration]domain.Resolution{
		30 * time.Second: domain.ResolutionRaw,
		5 * time.Minute:  domain.Resolution1m,
		3 * time.Hour:    domain.Resolution1h,
	} {
		if got, _ := resolutionFor(step); got != want {
			t.Errorf("resolutionFor(%v) = %v, want %v", step, got, want)
		}
	}
}

func TestREDValue(t *testing.T) {
	if v := domain.REDValue(domain.MeasureErrorRate, 0, 0, domain.Latency{}); v != 0 || math.IsNaN(v) {
		t.Errorf("error rate without requests = %v", v)
	}
	if v := domain.REDValue(domain.MeasureErrorRate, 4, 1, domain.Latency{}); v != 0.25 {
		t.Errorf("error rate = %v", v)
	}
}
