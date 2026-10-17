// Package ingestauth checks ingest API keys on HTTP receivers (OTLP/HTTP,
// Prometheus remote write).
package ingestauth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

// Middleware rejects requests without a valid key: 401 for a bad key, 503
// while keys are not loaded yet (so clients retry). A nil auth lets
// everything through.
func Middleware(auth port.IngestAuthorizer, next http.Handler) http.Handler {
	if auth == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := auth.Authorize(r.Context(), KeyFromHeader(r.Header)); err != nil {
			if errors.Is(err, domain.ErrUnavailable) {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="vindice"`)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// KeyFromHeader reads the API key from "Authorization: Bearer <key>" or,
// for clients that cannot set Authorization, "X-Vindice-Key: <key>".
func KeyFromHeader(h http.Header) string {
	if scheme, key, ok := strings.Cut(h.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(key)
	}
	return strings.TrimSpace(h.Get("X-Vindice-Key"))
}
