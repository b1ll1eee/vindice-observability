// Package service holds the application services that implement the inbound ports.
package service

import (
	"context"
	"fmt"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
	"github.com/b1ll1eee/vindice-observability/internal/core/port"
)

// IngestService hands validated payloads to the queue. It stays thin on
// purpose: decoding happens in the writer so ingest is cheap to scale.
type IngestService struct {
	pub port.Publisher
}

var _ port.Ingestor = (*IngestService)(nil)

func NewIngestService(pub port.Publisher) *IngestService {
	return &IngestService{pub: pub}
}

func (s *IngestService) Ingest(ctx context.Context, sig domain.Signal, payload []byte) error {
	if !sig.Valid() {
		return fmt.Errorf("%w: %q", domain.ErrInvalidSignal, sig)
	}
	if len(payload) == 0 {
		return nil
	}
	if err := s.pub.Publish(ctx, sig, payload); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
	return nil
}
