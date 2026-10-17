package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

// APIKeyService manages ingest keys and authorises ingest requests.
//
// Ingest checks keys against an in-memory set that Run refreshes, so a
// request never waits on ClickHouse and ingest keeps working through a
// ClickHouse outage. A revoked key therefore stops working on other
// instances within one refresh interval.
type APIKeyService struct {
	repo port.APIKeyRepository
	log  *slog.Logger
	now  func() time.Time

	mu     sync.RWMutex
	byHash map[string]string // hash -> key id, active keys only
	loaded bool

	usedMu sync.Mutex
	used   map[string]time.Time // key id -> last use, not yet written
}

var (
	_ port.IngestAuthorizer = (*APIKeyService)(nil)
	_ port.KeyManager       = (*APIKeyService)(nil)
)

func NewAPIKeyService(repo port.APIKeyRepository, log *slog.Logger) *APIKeyService {
	return &APIKeyService{repo: repo, log: log, now: time.Now, byHash: map[string]string{}, used: map[string]time.Time{}}
}

func (s *APIKeyService) Authorize(_ context.Context, key string) error {
	s.mu.RLock()
	loaded := s.loaded
	id, ok := s.byHash[domain.HashKey(key)]
	s.mu.RUnlock()
	switch {
	case !loaded:
		// Keys not loaded yet (ClickHouse unreachable at start): ask the
		// client to retry instead of rejecting data for good.
		return domain.ErrUnavailable
	case !strings.HasPrefix(key, domain.KeyPrefix) || !ok:
		return domain.ErrInvalidKey
	}
	s.usedMu.Lock()
	s.used[id] = s.now().Truncate(time.Minute)
	s.usedMu.Unlock()
	return nil
}

// Refresh reloads the active keys.
func (s *APIKeyService) Refresh(ctx context.Context) error {
	keys, err := s.repo.ListKeys(ctx)
	if err != nil {
		return err
	}
	m := make(map[string]string, len(keys))
	for _, k := range keys {
		if !k.Revoked {
			m[k.Hash] = k.ID
		}
	}
	s.mu.Lock()
	s.byHash, s.loaded = m, true
	s.mu.Unlock()
	return nil
}

// FlushUsage writes the last-use times collected since the previous flush.
func (s *APIKeyService) FlushUsage(ctx context.Context) error {
	s.usedMu.Lock()
	used := s.used
	s.used = map[string]time.Time{}
	s.usedMu.Unlock()
	if len(used) == 0 {
		return nil
	}
	if err := s.repo.RecordKeyUse(ctx, used); err != nil {
		s.usedMu.Lock() // keep them for the next attempt
		for id, t := range used {
			if s.used[id].Before(t) {
				s.used[id] = t
			}
		}
		s.usedMu.Unlock()
		return err
	}
	return nil
}

// Run refreshes keys and flushes usage every interval until ctx ends.
func (s *APIKeyService) Run(ctx context.Context, interval time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("refreshing API keys failed; using the previous set", "err", err)
		}
		if err := s.FlushUsage(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("recording API key use failed", "err", err)
		}
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.FlushUsage(flushCtx)
			return nil
		case <-t.C:
		}
	}
}

func (s *APIKeyService) ListKeys(ctx context.Context) ([]domain.APIKey, map[string]time.Time, error) {
	keys, err := s.repo.ListKeys(ctx)
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(keys, func(a, b domain.APIKey) int { return b.CreatedAt.Compare(a.CreatedAt) })
	used, err := s.repo.KeyLastUse(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Include uses this instance has not written yet.
	s.usedMu.Lock()
	for id, t := range s.used {
		if used[id].Before(t) {
			used[id] = t
		}
	}
	s.usedMu.Unlock()
	return keys, maps.Clone(used), nil
}

func (s *APIKeyService) CreateKey(ctx context.Context, name, createdBy string) (domain.APIKey, string, error) {
	name = strings.TrimSpace(name)
	if err := domain.ValidateKeyName(name); err != nil {
		return domain.APIKey{}, "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return domain.APIKey{}, "", err
	}
	secret := domain.KeyPrefix + base64.RawURLEncoding.EncodeToString(b)
	k := domain.APIKey{
		ID: newID(), Name: name, Hash: domain.HashKey(secret), Hint: secret[:len(domain.KeyPrefix)+4],
		CreatedBy: createdBy, CreatedAt: s.now().UTC(),
	}
	if err := s.repo.SaveKey(ctx, k); err != nil {
		return domain.APIKey{}, "", err
	}
	s.mu.Lock()
	s.byHash[k.Hash] = k.ID
	s.mu.Unlock()
	return k, secret, nil
}

func (s *APIKeyService) RevokeKey(ctx context.Context, id string) error {
	k, ok, err := s.repo.GetKey(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrNotFound
	}
	if !k.Revoked {
		k.Revoked, k.RevokedAt = true, s.now().UTC()
		if err := s.repo.SaveKey(ctx, k); err != nil {
			return err
		}
	}
	s.mu.Lock()
	delete(s.byHash, k.Hash)
	s.mu.Unlock()
	return nil
}
