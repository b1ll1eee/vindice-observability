package clickhouse

import (
	"context"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

var (
	_ port.UserRepository = (*Repository)(nil)
	_ port.SecretStore    = (*Repository)(nil)
)

func (r *Repository) ListUsers(ctx context.Context) ([]domain.User, error) {
	return listDocs[domain.User](ctx, r, "users")
}

func (r *Repository) GetUser(ctx context.Context, id string) (domain.User, bool, error) {
	return getDoc[domain.User](ctx, r, "users", id)
}

func (r *Repository) SaveUser(ctx context.Context, u domain.User) error {
	return putDoc(ctx, r, "users", u.ID, u, false)
}

func (r *Repository) DeleteUser(ctx context.Context, id string) error {
	return putDoc(ctx, r, "users", id, nil, true)
}

func (r *Repository) GetOrCreateSecret(ctx context.Context, name string, gen func() string) (string, error) {
	read := func() (string, bool, error) {
		var (
			v string
			n uint64
		)
		err := r.conn.QueryRow(readCtx(ctx, false),
			"SELECT argMin(value, created_at), count() FROM secrets WHERE name = ?", name).Scan(&v, &n)
		return v, n > 0, err
	}
	if v, ok, err := read(); err != nil || ok {
		return v, err
	}
	if err := r.conn.Exec(ctx, "INSERT INTO secrets (name, value, created_at) VALUES (?, ?, ?)",
		name, gen(), time.Now()); err != nil {
		return "", err
	}
	// Read back: if another instance raced us, the oldest value wins for both.
	v, _, err := read()
	return v, err
}

var _ port.APIKeyRepository = (*Repository)(nil)

func (r *Repository) ListKeys(ctx context.Context) ([]domain.APIKey, error) {
	return listDocs[domain.APIKey](ctx, r, "api_keys")
}

func (r *Repository) GetKey(ctx context.Context, id string) (domain.APIKey, bool, error) {
	return getDoc[domain.APIKey](ctx, r, "api_keys", id)
}

func (r *Repository) SaveKey(ctx context.Context, k domain.APIKey) error {
	return putDoc(ctx, r, "api_keys", k.ID, k, false)
}

func (r *Repository) RecordKeyUse(ctx context.Context, used map[string]time.Time) error {
	type use struct {
		id string
		at time.Time
	}
	rows := make([]use, 0, len(used))
	for id, at := range used {
		rows = append(rows, use{id, at})
	}
	return insert(ctx, r.conn, "INSERT INTO api_key_use (key_id, used_at)", rows,
		func(u *use) []any { return []any{u.id, u.at} })
}

func (r *Repository) KeyLastUse(ctx context.Context) (map[string]time.Time, error) {
	rows, err := r.conn.Query(readCtx(ctx, false), "SELECT key_id, max(used_at) FROM api_key_use GROUP BY key_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var (
			id string
			at time.Time
		)
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}
