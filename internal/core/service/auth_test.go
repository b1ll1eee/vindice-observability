package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

type memUsers struct{ m map[string]domain.User }

func (r *memUsers) ListUsers(context.Context) ([]domain.User, error) {
	var out []domain.User
	for _, u := range r.m {
		out = append(out, u)
	}
	return out, nil
}
func (r *memUsers) GetUser(_ context.Context, id string) (domain.User, bool, error) {
	u, ok := r.m[id]
	return u, ok, nil
}
func (r *memUsers) SaveUser(_ context.Context, u domain.User) error { r.m[u.ID] = u; return nil }
func (r *memUsers) DeleteUser(_ context.Context, id string) error   { delete(r.m, id); return nil }

// plainHasher keeps tests fast; real hashing is tested in authcrypto.
type plainHasher struct{}

func (plainHasher) Hash(p string) (string, error) { return "h:" + p, nil }
func (plainHasher) Verify(h, p string) bool       { return h == "h:"+p }

// fakeCodec encodes sessions readably; signing is tested in authcrypto.
type fakeCodec struct{}

func (fakeCodec) Encode(s domain.Session) string {
	return s.UserID + "|" + string(rune('0'+s.Version)) + "|" + s.Expires.Format(time.RFC3339)
}
func (fakeCodec) Decode(tok string, now time.Time) (domain.Session, error) {
	p := strings.Split(tok, "|")
	if len(p) != 3 {
		return domain.Session{}, errors.New("bad")
	}
	exp, _ := time.Parse(time.RFC3339, p[2])
	if !now.Before(exp) {
		return domain.Session{}, errors.New("expired")
	}
	return domain.Session{UserID: p[0], Version: int(p[1][0] - '0'), Expires: exp}, nil
}

const pw = "a-long-password"

func newAuth(t *testing.T) (*AuthService, *memUsers, *time.Time) {
	t.Helper()
	repo := &memUsers{m: map[string]domain.User{}}
	a, err := NewAuthService(repo, plainHasher{}, fakeCodec{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := t0
	a.now = func() time.Time { return now }
	return a, repo, &now
}

func TestBootstrapOnlyOnce(t *testing.T) {
	a, repo, _ := newAuth(t)
	gen, err := a.Bootstrap(context.Background(), "admin", "")
	if err != nil || len(gen) < 16 {
		t.Fatalf("bootstrap: %q %v", gen, err)
	}
	if _, _, err := a.Login(context.Background(), "admin", gen, "ip"); err != nil {
		t.Errorf("generated password does not work: %v", err)
	}
	if again, _ := a.Bootstrap(context.Background(), "admin", ""); again != "" || len(repo.m) != 1 {
		t.Error("bootstrap ran with users present")
	}
}

func TestLoginSessionLifecycle(t *testing.T) {
	a, _, now := newAuth(t)
	ctx := context.Background()
	u, err := a.CreateUser(ctx, "Bill", domain.RoleEditor, pw)
	if err != nil {
		t.Fatal(err)
	}

	tok, got, err := a.Login(ctx, "bill", pw, "ip") // usernames are case-insensitive
	if err != nil || got.ID != u.ID {
		t.Fatalf("login: %v", err)
	}
	if me, err := a.Authenticate(ctx, tok); err != nil || me.Role != domain.RoleEditor {
		t.Fatalf("authenticate: %+v %v", me, err)
	}

	*now = now.Add(2 * time.Hour)
	if _, err := a.Authenticate(ctx, tok); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expired session accepted: %v", err)
	}

	*now = t0
	if err := a.Logout(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(ctx, tok); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("session survived logout: %v", err)
	}
}

func TestPasswordChangeEndsOtherSessions(t *testing.T) {
	a, _, _ := newAuth(t)
	ctx := context.Background()
	u, _ := a.CreateUser(ctx, "bill", domain.RoleViewer, pw)
	tok, _, _ := a.Login(ctx, "bill", pw, "ip")

	if err := a.ChangePassword(ctx, u.ID, "wrong-current-pw", "another-long-pw"); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("wrong current password accepted: %v", err)
	}
	if err := a.ChangePassword(ctx, u.ID, pw, "short"); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("short password accepted: %v", err)
	}
	if err := a.ChangePassword(ctx, u.ID, pw, "another-long-pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(ctx, tok); err == nil {
		t.Error("old session survived password change")
	}
	if _, _, err := a.Login(ctx, "bill", "another-long-pw", "ip"); err != nil {
		t.Errorf("new password: %v", err)
	}
}

func TestLockout(t *testing.T) {
	a, _, now := newAuth(t)
	ctx := context.Background()
	_, _ = a.CreateUser(ctx, "bill", domain.RoleViewer, pw)

	for i := 0; i < maxFailures-1; i++ {
		if _, _, err := a.Login(ctx, "bill", "nope", "1.1.1.1"); !errors.Is(err, domain.ErrBadCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, _, err := a.Login(ctx, "bill", "nope", "1.1.1.1"); !errors.Is(err, domain.ErrLockedOut) {
		t.Fatalf("fifth failure should lock: %v", err)
	}
	if _, _, err := a.Login(ctx, "bill", pw, "1.1.1.1"); !errors.Is(err, domain.ErrLockedOut) {
		t.Errorf("correct password during lockout: %v", err)
	}
	if _, _, err := a.Login(ctx, "bill", pw, "2.2.2.2"); err != nil {
		t.Errorf("other address should not be locked: %v", err)
	}
	*now = now.Add(lockout + time.Second)
	if _, _, err := a.Login(ctx, "bill", pw, "1.1.1.1"); err != nil {
		t.Errorf("after lockout: %v", err)
	}
	if _, _, err := a.Login(ctx, "ghost", pw, "3.3.3.3"); !errors.Is(err, domain.ErrBadCredentials) {
		t.Errorf("unknown user: %v", err)
	}
}

func TestAdminGuards(t *testing.T) {
	a, _, _ := newAuth(t)
	ctx := context.Background()
	admin, _ := a.CreateUser(ctx, "admin", domain.RoleAdmin, pw)
	bob, _ := a.CreateUser(ctx, "bob", domain.RoleViewer, pw)

	if _, err := a.CreateUser(ctx, "BOB", domain.RoleViewer, pw); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("duplicate username accepted: %v", err)
	}
	if _, err := a.CreateUser(ctx, "x y", domain.RoleViewer, pw); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("bad username accepted: %v", err)
	}
	if _, err := a.UpdateUser(ctx, bob.ID, admin.ID, domain.RoleViewer, false, ""); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("last admin demoted: %v", err)
	}
	if err := a.DeleteUser(ctx, bob.ID, admin.ID); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("last admin deleted: %v", err)
	}
	if err := a.DeleteUser(ctx, admin.ID, admin.ID); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("self delete allowed: %v", err)
	}

	// Disabling signs the user out immediately.
	tok, _, _ := a.Login(ctx, "bob", pw, "ip")
	if _, err := a.UpdateUser(ctx, admin.ID, bob.ID, domain.RoleViewer, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(ctx, tok); err == nil {
		t.Error("disabled user still signed in")
	}
	if _, _, err := a.Login(ctx, "bob", pw, "ip2"); !errors.Is(err, domain.ErrBadCredentials) {
		t.Errorf("disabled user signed in: %v", err)
	}

	// With a second admin, the first can be demoted.
	if _, err := a.UpdateUser(ctx, admin.ID, bob.ID, domain.RoleAdmin, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateUser(ctx, bob.ID, admin.ID, domain.RoleEditor, false, ""); err != nil {
		t.Errorf("demote with another admin present: %v", err)
	}
}

func TestRoleAllows(t *testing.T) {
	if !domain.RoleAdmin.Allows(domain.RoleEditor) || domain.RoleViewer.Allows(domain.RoleEditor) || !domain.RoleEditor.Allows(domain.RoleViewer) {
		t.Error("role ordering wrong")
	}
}
