// Package otlpmap maps OTLP export requests onto domain entities.
package otlpmap

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

const unknownService = "unknown_service"

// resources collects the distinct resources seen in one request.
type resources struct {
	byID map[uint64]struct{}
	list []domain.Resource
}

func (r *resources) add(res *resourcepb.Resource) domain.Resource {
	attrs := attrMap(res.GetAttributes())
	svc := attrs["service.name"]
	if svc == "" {
		svc = unknownService
	}
	out := domain.Resource{ID: hashAttrs(0, "", attrs), ServiceName: svc, Attributes: attrs}
	if r.byID == nil {
		r.byID = map[uint64]struct{}{}
	}
	if _, ok := r.byID[out.ID]; !ok {
		r.byID[out.ID] = struct{}{}
		r.list = append(r.list, out)
	}
	return out
}

// hashAttrs returns a stable hash of seed, name and the attributes, independent
// of map order. Used for resource and series IDs.
func hashAttrs(seed uint64, name string, attrs map[string]string) uint64 {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	d := xxhash.New()
	var b [8]byte
	for i := range 8 {
		b[i] = byte(seed >> (8 * i))
	}
	_, _ = d.Write(b[:])
	_, _ = d.WriteString(name)
	for _, k := range keys {
		_, _ = d.WriteString("\x00")
		_, _ = d.WriteString(k)
		_, _ = d.WriteString("\x01")
		_, _ = d.WriteString(attrs[k])
	}
	return d.Sum64()
}

func attrMap(kvs []*commonpb.KeyValue) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[kv.GetKey()] = AnyValueString(kv.GetValue())
	}
	return m
}

// AnyValueString renders an OTLP AnyValue as a string. Arrays and maps become JSON.
func AnyValueString(v *commonpb.AnyValue) string {
	switch x := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	case *commonpb.AnyValue_BoolValue:
		return strconv.FormatBool(x.BoolValue)
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(x.IntValue, 10)
	case *commonpb.AnyValue_DoubleValue:
		return strconv.FormatFloat(x.DoubleValue, 'g', -1, 64)
	case *commonpb.AnyValue_BytesValue:
		return base64.StdEncoding.EncodeToString(x.BytesValue)
	case *commonpb.AnyValue_ArrayValue, *commonpb.AnyValue_KvlistValue:
		b, err := json.Marshal(anyValueJSON(v))
		if err != nil {
			return ""
		}
		return string(b)
	}
	return ""
}

func anyValueJSON(v *commonpb.AnyValue) any {
	switch x := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	case *commonpb.AnyValue_BoolValue:
		return x.BoolValue
	case *commonpb.AnyValue_IntValue:
		return x.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return x.DoubleValue
	case *commonpb.AnyValue_BytesValue:
		return base64.StdEncoding.EncodeToString(x.BytesValue)
	case *commonpb.AnyValue_ArrayValue:
		out := make([]any, 0, len(x.ArrayValue.GetValues()))
		for _, e := range x.ArrayValue.GetValues() {
			out = append(out, anyValueJSON(e))
		}
		return out
	case *commonpb.AnyValue_KvlistValue:
		out := make(map[string]any, len(x.KvlistValue.GetValues()))
		for _, kv := range x.KvlistValue.GetValues() {
			out[kv.GetKey()] = anyValueJSON(kv.GetValue())
		}
		return out
	}
	return nil
}

func unixNano(ns uint64) time.Time { return time.Unix(0, int64(ns)).UTC() }

func traceID(b []byte) (id [16]byte) {
	copy(id[:], b)
	return id
}

func spanID(b []byte) (id [8]byte) {
	copy(id[:], b)
	return id
}

// NormalizeSeverity fills whichever of severity number/text is missing.
// Number 0 means unknown.
func NormalizeSeverity(num uint8, text string) (uint8, string) {
	if num == 0 && text != "" {
		num = severityFromText(text)
	}
	if text == "" && num != 0 {
		text = severityText(num)
	}
	return num, text
}

func severityText(n uint8) string {
	switch {
	case n == 0:
		return ""
	case n <= 4:
		return "TRACE"
	case n <= 8:
		return "DEBUG"
	case n <= 12:
		return "INFO"
	case n <= 16:
		return "WARN"
	case n <= 20:
		return "ERROR"
	default:
		return "FATAL"
	}
}

func severityFromText(t string) uint8 {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "TRACE":
		return 1
	case "DEBUG":
		return 5
	case "INFO", "INFORMATION", "NOTICE":
		return 9
	case "WARN", "WARNING":
		return 13
	case "ERROR", "ERR":
		return 17
	case "FATAL", "CRITICAL", "CRIT", "PANIC", "EMERGENCY", "ALERT":
		return 21
	}
	return 0
}
