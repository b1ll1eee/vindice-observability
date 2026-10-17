package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

type memKeys struct {
	keys    map[string]domain.APIKey
	use     map[string]time.Time
	listErr error
	useErr  error
}

func (m *memKeys) ListKeys(context.Context) ([]domain.APIKey, error) {
	var out []domain.APIKey
	for _, k := range m.keys {
		out = append(out, k)
	}
	return out, m.listErr
}
func (m *memKeys) GetKey(_ context.Context, id string) (domain.APIKey, bool, error) {
	k, ok := m.keys[id]
	return k, ok, nil
}
func (m *memKeys) SaveKey(_ context.Context, k domain.APIKey) error { m.keys[k.ID] = k; return nil }
func (m *memKeys) RecordKeyUse(_ context.Context, used map[string]time.Time) error {
	if m.useErr != nil {
		return m.useErr
	}
	for id, t := range used {
		m.use[id] = t
	}
	return nil
}
func (m *memKeys) KeyLastUse(context.Context) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for k, v := range m.use {
		out[k] = v
	}
	return out, nil
}

func newKeys() (*APIKeyService, *memKeys) {
	repo := &memKeys{keys: map[string]domain.APIKey{}, use: map[string]time.Time{}}
	s := NewAPIKeyService(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = func() time.Time { return t0 }
	return s, repo
}

func TestKeyLifecycle(t *testing.T) {
	s, repo := newKeys()
	ctx := context.Background()

	if err := s.Authorize(ctx, "anything"); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("before the first refresh ingest must be retryable: %v", err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	k, secret, err := s.CreateKey(ctx, " ci pipeline ", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, domain.KeyPrefix) || len(secret) < 40 || k.Name != "ci pipeline" {
		t.Errorf("key %+v, secret length %d", k, len(secret))
	}
	if strings.Contains(repo.keys[k.ID].Hash, secret) || repo.keys[k.ID].Hash != domain.HashKey(secret) {
		t.Error("store must hold only the hash")
	}
	if !strings.HasPrefix(secret, k.Hint) {
		t.Errorf("hint %q is not a prefix of the secret", k.Hint)
	}

	if err := s.Authorize(ctx, secret); err != nil {
		t.Errorf("new key rejected: %v", err)
	}
	for _, bad := range []string{"", "vdc_wrong", secret + "x", strings.TrimPrefix(secret, domain.KeyPrefix)} {
		if err := s.Authorize(ctx, bad); !errors.Is(err, domain.ErrInvalidKey) {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}

	if err := s.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}
	if !repo.use[k.ID].Equal(t0.Truncate(time.Minute)) {
		t.Errorf("last use = %v", repo.use[k.ID])
	}

	if err := s.RevokeKey(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, secret); !errors.Is(err, domain.ErrInvalidKey) {
		t.Errorf("revoked key accepted: %v", err)
	}
	if !repo.keys[k.ID].Revoked {
		t.Error("revoke not stored")
	}
	if err := s.RevokeKey(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("revoke missing: %v", err)
	}
}

func TestKeyRefreshPicksUpOtherInstances(t *testing.T) {
	writer, repo := newKeys()
	ingest := NewAPIKeyService(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	_ = ingest.Refresh(ctx)

	k, secret, _ := writer.CreateKey(ctx, "svc", "admin")
	if err := ingest.Authorize(ctx, secret); !errors.Is(err, domain.ErrInvalidKey) {
		t.Fatalf("unexpected: other instance knows the key before refresh: %v", err)
	}
	_ = ingest.Refresh(ctx)
	if err := ingest.Authorize(ctx, secret); err != nil {
		t.Errorf("after refresh: %v", err)
	}
	_ = writer.RevokeKey(ctx, k.ID)
	_ = ingest.Refresh(ctx)
	if err := ingest.Authorize(ctx, secret); !errors.Is(err, domain.ErrInvalidKey) {
		t.Errorf("revoke not picked up: %v", err)
	}

	// A failed refresh keeps the previous set rather than locking everyone out.
	_, secret2, _ := writer.CreateKey(ctx, "svc2", "admin")
	_ = ingest.Refresh(ctx)
	repo.listErr = errors.New("clickhouse down")
	if err := ingest.Refresh(ctx); err == nil {
		t.Fatal("expected refresh error")
	}
	if err := ingest.Authorize(ctx, secret2); err != nil {
		t.Errorf("keys lost on failed refresh: %v", err)
	}
}

func TestKeyUsageKeptWhenFlushFails(t *testing.T) {
	s, repo := newKeys()
	ctx := context.Background()
	_ = s.Refresh(ctx)
	k, secret, _ := s.CreateKey(ctx, "svc", "admin")
	_ = s.Authorize(ctx, secret)

	repo.useErr = errors.New("down")
	if err := s.FlushUsage(ctx); err == nil {
		t.Fatal("expected flush error")
	}
	repo.useErr = nil
	if err := s.FlushUsage(ctx); err != nil || repo.use[k.ID].IsZero() {
		t.Errorf("usage lost after a failed flush: %v", repo.use)
	}
}

func TestKeyNameRequired(t *testing.T) {
	s, _ := newKeys()
	if _, _, err := s.CreateKey(context.Background(), "   ", "admin"); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("blank key name accepted: %v", err)
	}
}
