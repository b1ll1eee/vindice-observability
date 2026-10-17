// Package promwrite receives Prometheus remote write 1.0 and hands it on as
// OTLP metrics, so everything after ingest treats it like any other metric.
package promwrite

import (
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// A hand-written decoder for the few prometheus.WriteRequest fields used
// here, instead of importing the whole Prometheus module.

type writeRequest struct {
	series   []timeSeries
	metadata []metricMetadata
}

type timeSeries struct {
	labels     []label
	samples    []sample
	histograms int // native histograms are counted, not stored
}

type label struct{ name, value string }

type sample struct {
	value float64
	ts    int64 // milliseconds
}

// metricMetadata.typ follows prometheus.MetricMetadata.MetricType.
type metricMetadata struct {
	typ    int32
	family string
	unit   string
}

const (
	metaCounter   = 1
	metaGauge     = 2
	metaHistogram = 3
	metaSummary   = 5
)

func decodeWriteRequest(b []byte) (writeRequest, error) {
	var wr writeRequest
	err := fields(b, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
		switch {
		case num == 1 && typ == protowire.BytesType:
			ts, err := decodeTimeSeries(v)
			wr.series = append(wr.series, ts)
			return err
		case num == 3 && typ == protowire.BytesType:
			m, err := decodeMetadata(v)
			wr.metadata = append(wr.metadata, m)
			return err
		}
		return nil
	})
	return wr, err
}

func decodeTimeSeries(b []byte) (timeSeries, error) {
	var ts timeSeries
	err := fields(b, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
		switch {
		case num == 1 && typ == protowire.BytesType:
			var l label
			err := fields(v, func(n protowire.Number, t protowire.Type, s []byte, _ uint64) error {
				if t == protowire.BytesType {
					switch n {
					case 1:
						l.name = string(s)
					case 2:
						l.value = string(s)
					}
				}
				return nil
			})
			ts.labels = append(ts.labels, l)
			return err
		case num == 2 && typ == protowire.BytesType:
			var s sample
			err := fields(v, func(n protowire.Number, t protowire.Type, _ []byte, x uint64) error {
				switch {
				case n == 1 && t == protowire.Fixed64Type:
					s.value = math.Float64frombits(x)
				case n == 2 && t == protowire.VarintType:
					s.ts = int64(x)
				}
				return nil
			})
			ts.samples = append(ts.samples, s)
			return err
		case num == 4:
			ts.histograms++
		}
		return nil
	})
	return ts, err
}

func decodeMetadata(b []byte) (metricMetadata, error) {
	var m metricMetadata
	err := fields(b, func(num protowire.Number, typ protowire.Type, v []byte, x uint64) error {
		switch {
		case num == 1 && typ == protowire.VarintType:
			m.typ = int32(x)
		case num == 2 && typ == protowire.BytesType:
			m.family = string(v)
		case num == 5 && typ == protowire.BytesType:
			m.unit = string(v)
		}
		return nil
	})
	return m, err
}

// fields walks the top-level fields of a message. For length-delimited
// fields v is the payload; for numeric fields x is the value.
func fields(b []byte, fn func(num protowire.Number, typ protowire.Type, v []byte, x uint64) error) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return fmt.Errorf("protobuf tag: %w", protowire.ParseError(n))
		}
		b = b[n:]
		var (
			v []byte
			x uint64
		)
		switch typ {
		case protowire.BytesType:
			v, n = protowire.ConsumeBytes(b)
		case protowire.VarintType:
			x, n = protowire.ConsumeVarint(b)
		case protowire.Fixed64Type:
			x, n = protowire.ConsumeFixed64(b)
		case protowire.Fixed32Type:
			var x32 uint32
			x32, n = protowire.ConsumeFixed32(b)
			x = uint64(x32)
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
		}
		if n < 0 {
			return fmt.Errorf("protobuf field %d: %w", num, protowire.ParseError(n))
		}
		b = b[n:]
		if err := fn(num, typ, v, x); err != nil {
			return err
		}
	}
	return nil
}
