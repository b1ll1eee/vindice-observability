package otlpmap

import (
	"maps"
	"strconv"

	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

type MetricsResult struct {
	Series    []domain.Series
	Samples   []domain.Sample
	Resources []domain.Resource
	// Skipped counts data points of unsupported types (exponential histograms).
	Skipped int
}

// MapMetrics flattens metrics into Prometheus-style series: histograms become
// <name>_bucket{le}, <name>_sum and <name>_count; summaries become
// <name>{quantile}, <name>_sum and <name>_count.
func MapMetrics(req *colmetrics.ExportMetricsServiceRequest) MetricsResult {
	var (
		out  MetricsResult
		res  resources
		seen = map[uint64]struct{}{}
	)
	for _, rm := range req.GetResourceMetrics() {
		r := res.add(rm.GetResource())
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				b := seriesBuilder{out: &out, seen: seen, res: r, unit: m.GetUnit()}
				switch d := m.GetData().(type) {
				case *metricspb.Metric_Gauge:
					for _, dp := range d.Gauge.GetDataPoints() {
						b.add(m.GetName(), "gauge", metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_UNSPECIFIED, false,
							attrMap(dp.GetAttributes()), dp.GetTimeUnixNano(), numberValue(dp))
					}
				case *metricspb.Metric_Sum:
					for _, dp := range d.Sum.GetDataPoints() {
						b.add(m.GetName(), "sum", d.Sum.GetAggregationTemporality(), d.Sum.GetIsMonotonic(),
							attrMap(dp.GetAttributes()), dp.GetTimeUnixNano(), numberValue(dp))
					}
				case *metricspb.Metric_Histogram:
					for _, dp := range d.Histogram.GetDataPoints() {
						b.histogram(m.GetName(), d.Histogram.GetAggregationTemporality(), dp)
					}
				case *metricspb.Metric_Summary:
					for _, dp := range d.Summary.GetDataPoints() {
						b.summary(m.GetName(), dp)
					}
				case *metricspb.Metric_ExponentialHistogram:
					out.Skipped += len(d.ExponentialHistogram.GetDataPoints())
				}
			}
		}
	}
	out.Resources = res.list
	return out
}

type seriesBuilder struct {
	out  *MetricsResult
	seen map[uint64]struct{}
	res  domain.Resource
	unit string
}

func (b seriesBuilder) add(name, typ string, temp metricspb.AggregationTemporality, monotonic bool,
	attrs map[string]string, tsNano uint64, v float64) {
	id := hashAttrs(b.res.ID, name, attrs)
	if _, ok := b.seen[id]; !ok {
		b.seen[id] = struct{}{}
		b.out.Series = append(b.out.Series, domain.Series{
			ID:          id,
			MetricName:  name,
			ServiceName: b.res.ServiceName,
			Type:        typ,
			Temporality: temporality(temp),
			IsMonotonic: monotonic,
			Unit:        b.unit,
			ResourceID:  b.res.ID,
			Attributes:  attrs,
		})
	}
	b.out.Samples = append(b.out.Samples, domain.Sample{SeriesID: id, Timestamp: unixNano(tsNano), Value: v})
}

func (b seriesBuilder) histogram(name string, temp metricspb.AggregationTemporality, dp *metricspb.HistogramDataPoint) {
	attrs := attrMap(dp.GetAttributes())
	ts := dp.GetTimeUnixNano()
	monotonic := temp == metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE

	// OTLP bucket counts are per bucket; Prometheus buckets are cumulative.
	var cum uint64
	bounds := dp.GetExplicitBounds()
	for i, c := range dp.GetBucketCounts() {
		cum += c
		le := "+Inf"
		if i < len(bounds) {
			le = strconv.FormatFloat(bounds[i], 'g', -1, 64)
		}
		b.add(name+"_bucket", "histogram", temp, monotonic, withAttr(attrs, "le", le), ts, float64(cum))
	}
	b.add(name+"_sum", "histogram", temp, monotonic, attrs, ts, dp.GetSum())
	b.add(name+"_count", "histogram", temp, monotonic, attrs, ts, float64(dp.GetCount()))
}

func (b seriesBuilder) summary(name string, dp *metricspb.SummaryDataPoint) {
	attrs := attrMap(dp.GetAttributes())
	ts := dp.GetTimeUnixNano()
	cumulative := metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE
	for _, q := range dp.GetQuantileValues() {
		b.add(name, "summary", cumulative, false,
			withAttr(attrs, "quantile", strconv.FormatFloat(q.GetQuantile(), 'g', -1, 64)), ts, q.GetValue())
	}
	b.add(name+"_sum", "summary", cumulative, true, attrs, ts, dp.GetSum())
	b.add(name+"_count", "summary", cumulative, true, attrs, ts, float64(dp.GetCount()))
}

func withAttr(attrs map[string]string, k, v string) map[string]string {
	m := maps.Clone(attrs)
	m[k] = v
	return m
}

func numberValue(dp *metricspb.NumberDataPoint) float64 {
	switch v := dp.GetValue().(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		return v.AsDouble
	case *metricspb.NumberDataPoint_AsInt:
		return float64(v.AsInt)
	}
	return 0
}

func temporality(t metricspb.AggregationTemporality) string {
	switch t {
	case metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA:
		return "delta"
	case metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE:
		return "cumulative"
	}
	return "unspecified"
}
