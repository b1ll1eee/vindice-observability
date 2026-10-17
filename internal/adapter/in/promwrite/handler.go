package promwrite

import (
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/klauspost/compress/snappy"
	"google.golang.org/protobuf/proto"

	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/ingestauth"
	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

type handler struct {
	ingest  port.Ingestor
	maxBody int64
	log     *slog.Logger
}

// New returns the remote write 1.0 handler (mount it at POST /api/v1/write).
// auth checks the API key; nil accepts requests without one.
func New(ingest port.Ingestor, auth port.IngestAuthorizer, maxBody int64, log *slog.Logger) http.Handler {
	h := &handler{ingest: ingest, maxBody: maxBody, log: log}
	return ingestauth.Middleware(auth, http.HandlerFunc(h.write))
}

func (h *handler) write(w http.ResponseWriter, r *http.Request) {
	ct, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "" && ct != "application/x-protobuf" {
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}
	if p := params["proto"]; p != "" && p != "prometheus.WriteRequest" {
		http.Error(w, "only remote write 1.0 is supported; set protobuf_message: prometheus.WriteRequest",
			http.StatusUnsupportedMediaType)
		return
	}
	if enc := r.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "snappy") {
		http.Error(w, "unsupported content encoding", http.StatusUnsupportedMediaType)
		return
	}

	compressed, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBody))
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	// Bound the decoded size too: a small snappy block can claim to be huge.
	if n, err := snappy.DecodedLen(compressed); err != nil || int64(n) > 4*h.maxBody {
		http.Error(w, "invalid or oversized snappy payload", http.StatusBadRequest)
		return
	}
	raw, err := snappy.Decode(nil, compressed)
	if err != nil {
		http.Error(w, "invalid snappy payload", http.StatusBadRequest)
		return
	}
	wr, err := decodeWriteRequest(raw)
	if err != nil {
		http.Error(w, "invalid WriteRequest: "+err.Error(), http.StatusBadRequest)
		return
	}

	req, st := toOTLP(wr)
	if st.histograms > 0 {
		h.log.Debug("skipped native histograms", "count", st.histograms)
	}
	if st.samples > 0 {
		payload, err := proto.Marshal(req)
		if err != nil {
			http.Error(w, "encode", http.StatusInternalServerError)
			return
		}
		if err := h.ingest.Ingest(r.Context(), domain.SignalMetrics, payload); err != nil {
			h.log.Warn("remote write ingest failed", "err", err)
			if errors.Is(err, domain.ErrUnavailable) {
				// Prometheus retries 5xx with backoff.
				http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
