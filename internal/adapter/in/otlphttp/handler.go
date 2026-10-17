// Package otlphttp is the OTLP/HTTP receiver (protobuf and JSON, optional gzip).
package otlphttp

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

const (
	contentProto = "application/x-protobuf"
	contentJSON  = "application/json"
)

type Handler struct {
	ingest  port.Ingestor
	auth    port.IngestAuthorizer
	maxBody int64
	log     *slog.Logger
}

// New returns the OTLP/HTTP receiver. auth checks the API key of every
// request; nil accepts requests without a key.
func New(ingest port.Ingestor, auth port.IngestAuthorizer, maxBody int64, log *slog.Logger) http.Handler {
	h := &Handler{ingest: ingest, auth: auth, maxBody: maxBody, log: log}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/logs", h.endpoint(domain.SignalLogs,
		func() proto.Message { return &collogs.ExportLogsServiceRequest{} },
		func() proto.Message { return &collogs.ExportLogsServiceResponse{} }))
	mux.Handle("POST /v1/traces", h.endpoint(domain.SignalTraces,
		func() proto.Message { return &coltrace.ExportTraceServiceRequest{} },
		func() proto.Message { return &coltrace.ExportTraceServiceResponse{} }))
	mux.Handle("POST /v1/metrics", h.endpoint(domain.SignalMetrics,
		func() proto.Message { return &colmetrics.ExportMetricsServiceRequest{} },
		func() proto.Message { return &colmetrics.ExportMetricsServiceResponse{} }))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}

func (h *Handler) endpoint(sig domain.Signal, newReq, newResp func() proto.Message) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.auth != nil {
			if err := h.auth.Authorize(r.Context(), KeyFromHeader(r.Header)); err != nil {
				if errors.Is(err, domain.ErrUnavailable) {
					w.Header().Set("Retry-After", "5")
					http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("WWW-Authenticate", `Bearer realm="vindice"`)
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
		}
		ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if ct == "" {
			ct = contentProto
		}
		if ct != contentProto && ct != contentJSON {
			http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
			return
		}

		body, err := h.readBody(w, r)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errTooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, err.Error(), status)
			return
		}

		// Validate, and normalise JSON to protobuf so the queue holds one format.
		req := newReq()
		if ct == contentJSON {
			if body, err = otlpJSONToProto(body, req); err != nil {
				http.Error(w, "invalid OTLP JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
		} else if err := proto.Unmarshal(body, req); err != nil {
			http.Error(w, "invalid OTLP protobuf", http.StatusBadRequest)
			return
		}

		if proto.Size(req) > 0 {
			if err := h.ingest.Ingest(r.Context(), sig, body); err != nil {
				h.log.Warn("ingest failed", "signal", sig, "err", err)
				if errors.Is(err, domain.ErrUnavailable) {
					// OTLP clients retry 503 with backoff.
					w.Header().Set("Retry-After", "5")
					http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		var out []byte
		if ct == contentJSON {
			out, _ = protojson.Marshal(newResp())
		} else {
			out, _ = proto.Marshal(newResp())
		}
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(out)
	})
}

var errTooLarge = errors.New("request body too large")

// KeyFromHeader reads the API key from "Authorization: Bearer <key>" or,
// for clients that cannot set Authorization, "X-Vindice-Key: <key>".
func KeyFromHeader(h http.Header) string {
	if scheme, key, ok := strings.Cut(h.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(key)
	}
	return strings.TrimSpace(h.Get("X-Vindice-Key"))
}

func (h *Handler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	var rd io.Reader = http.MaxBytesReader(w, r.Body, h.maxBody)
	switch r.Header.Get("Content-Encoding") {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(rd)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		rd = gz
	default:
		return nil, errors.New("unsupported content encoding")
	}
	// Limit the decompressed size too, against gzip bombs.
	b, err := io.ReadAll(io.LimitReader(rd, h.maxBody+1))
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) || int64(len(b)) > h.maxBody {
		return nil, errTooLarge
	}
	return b, err
}

// otlpJSONToProto parses OTLP/JSON into req and returns its protobuf
// encoding. OTLP/JSON writes trace and span IDs as hex, whereas protojson
// expects base64 for bytes fields, so IDs are re-encoded first.
func otlpJSONToProto(body []byte, req proto.Message) ([]byte, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	hexIDsToBase64(v)
	fixed, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(fixed, req); err != nil {
		return nil, err
	}
	return proto.Marshal(req)
}

var idKeys = map[string]bool{"traceId": true, "spanId": true, "parentSpanId": true}

func hexIDsToBase64(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if s, ok := val.(string); ok && idKeys[k] {
				if b, err := hex.DecodeString(s); err == nil {
					x[k] = base64.StdEncoding.EncodeToString(b)
				}
				continue
			}
			hexIDsToBase64(val)
		}
	case []any:
		for _, e := range x {
			hexIDsToBase64(e)
		}
	}
}
