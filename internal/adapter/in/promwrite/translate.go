package promwrite

import (
	"math"
	"slices"
	"strings"

	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

// staleNaN is Prometheus' staleness marker; it is not a real sample.
const staleNaN = 0x7ff0000000000002

type stats struct {
	samples    int
	histograms int // native histograms, skipped
}

// toOTLP maps remote-write series to OTLP metrics the way the OpenTelemetry
// Prometheus receiver does: job -> service.name, instance ->
// service.instance.id, every other label -> data point attribute.
func toOTLP(wr writeRequest) (*colmetrics.ExportMetricsServiceRequest, stats) {
	types := make(map[string]int32, len(wr.metadata))
	units := make(map[string]string, len(wr.metadata))
	for _, m := range wr.metadata {
		types[m.family], units[m.family] = m.typ, m.unit
	}

	var st stats
	byResource := map[[2]string]*metricspb.ScopeMetrics{}
	var order [][2]string
	for _, ts := range wr.series {
		st.histograms += ts.histograms
		var (
			name, job, instance string
			attrs               []*commonpb.KeyValue
		)
		for _, l := range ts.labels {
			switch l.name {
			case "__name__":
				name = l.value
			case "job":
				job = l.value
			case "instance":
				instance = l.value
			default:
				attrs = append(attrs, kv(l.name, l.value))
			}
		}
		if name == "" {
			continue
		}

		points := make([]*metricspb.NumberDataPoint, 0, len(ts.samples))
		for _, s := range ts.samples {
			if math.Float64bits(s.value) == staleNaN {
				continue
			}
			points = append(points, &metricspb.NumberDataPoint{
				Attributes:   attrs,
				TimeUnixNano: uint64(s.ts) * 1e6,
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: s.value},
			})
		}
		if len(points) == 0 {
			continue
		}
		st.samples += len(points)

		key := [2]string{job, instance}
		sm := byResource[key]
		if sm == nil {
			sm = &metricspb.ScopeMetrics{Scope: &commonpb.InstrumentationScope{Name: "prometheus-remote-write"}}
			byResource[key] = sm
			order = append(order, key)
		}
		m := &metricspb.Metric{Name: name, Unit: unitOf(name, units)}
		if isCounter(name, types) {
			m.Data = &metricspb.Metric_Sum{Sum: &metricspb.Sum{
				DataPoints:             points,
				AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
				IsMonotonic:            true,
			}}
		} else {
			m.Data = &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: points}}
		}
		sm.Metrics = append(sm.Metrics, m)
	}

	req := &colmetrics.ExportMetricsServiceRequest{}
	for _, key := range order {
		var res []*commonpb.KeyValue
		if key[0] != "" {
			res = append(res, kv("service.name", key[0]))
		}
		if key[1] != "" {
			res = append(res, kv("service.instance.id", key[1]))
		}
		req.ResourceMetrics = append(req.ResourceMetrics, &metricspb.ResourceMetrics{
			Resource:     &resourcepb.Resource{Attributes: res},
			ScopeMetrics: []*metricspb.ScopeMetrics{byResource[key]},
		})
	}
	return req, st
}

// isCounter decides between a monotonic cumulative sum and a gauge, from the
// family's metadata when Prometheus sent it, else from the name's suffix.
func isCounter(name string, types map[string]int32) bool {
	for _, suffix := range []string{"", "_total", "_bucket", "_count", "_sum"} {
		family, ok := strings.CutSuffix(name, suffix)
		if !ok {
			continue
		}
		switch types[family] {
		case metaCounter:
			return true
		case metaGauge:
			return false
		case metaHistogram:
			return suffix != ""
		case metaSummary:
			return suffix == "_count" || suffix == "_sum"
		}
	}
	return slices.ContainsFunc([]string{"_total", "_bucket", "_count", "_sum"}, func(s string) bool {
		return strings.HasSuffix(name, s)
	})
}

func unitOf(name string, units map[string]string) string {
	for _, suffix := range []string{"", "_total", "_bucket", "_count", "_sum"} {
		if family, ok := strings.CutSuffix(name, suffix); ok && units[family] != "" {
			return units[family]
		}
	}
	return ""
}

func kv(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}
