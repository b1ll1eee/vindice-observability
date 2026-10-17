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
