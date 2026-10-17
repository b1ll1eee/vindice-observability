package policy

import (
	"strconv"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

// PromoteSpanAttributes moves frequently queried HTTP attributes out of the
// attribute map into dedicated columns, which compress and filter far better.
// Both the current and the legacy semantic-convention keys are recognised.
func PromoteSpanAttributes(s *domain.SpanRow) {
	s.HTTPMethod = take(s.Attributes, "http.request.method", "http.method")
	s.HTTPRoute = take(s.Attributes, "http.route")
	if v := take(s.Attributes, "http.response.status_code", "http.status_code"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 16); err == nil {
			s.HTTPStatus = uint16(n)
		}
	}
}

// take returns the first non-empty value among keys and deletes all of them.
func take(m map[string]string, keys ...string) string {
	var out string
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if out == "" {
				out = v
			}
			delete(m, k)
		}
	}
	return out
}
