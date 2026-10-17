package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

// fakeAuth accepts the tokens "viewer", "editor" and "admin".
type fakeAuth struct{ loginErr error }

func (f *fakeAuth) Login(_ context.Context, username, password, _ string) (string, domain.User, error) {
	if f.loginErr != nil {
		return "", domain.User{}, f.loginErr
	}
	return username, domain.User{ID: username, Username: username, Role: domain.Role(username)}, nil
}
func (f *fakeAuth) Authenticate(_ context.Context, token string) (domain.User, error) {
	r := domain.Role(token)
	if !r.Valid() {
		return domain.User{}, domain.ErrUnauthenticated
	}
	return domain.User{ID: token, Username: token, Role: r, PasswordHash: "secret-hash"}, nil
}
func (f *fakeAuth) Logout(context.Context, string) error                         { return nil }
func (f *fakeAuth) ChangePassword(context.Context, string, string, string) error { return nil }

type fakeUsers struct{}

func (fakeUsers) ListUsers(context.Context) ([]domain.User, error) {
	return []domain.User{{ID: "1", Username: "a", Role: domain.RoleAdmin, PasswordHash: "secret-hash"}}, nil
}
func (fakeUsers) CreateUser(context.Context, string, domain.Role, string) (domain.User, error) {
	return domain.User{}, nil
}
func (fakeUsers) UpdateUser(context.Context, string, string, domain.Role, bool, string) (domain.User, error) {
	return domain.User{}, nil
}
func (fakeUsers) DeleteUser(context.Context, string, string) error { return nil }

// fakeDashboards rejects every save, so a 400 proves the guard let it through.
type fakeDashboards struct{}

func (fakeDashboards) ListDashboards(context.Context) ([]domain.Dashboard, error) { return nil, nil }
func (fakeDashboards) GetDashboard(context.Context, string) (domain.Dashboard, error) {
	return domain.Dashboard{}, nil
}
func (fakeDashboards) SaveDashboard(context.Context, domain.Dashboard) (domain.Dashboard, error) {
	return domain.Dashboard{}, domain.ErrInvalidQuery
}
func (fakeDashboards) DeleteDashboard(context.Context, string) error { return nil }

func authed(auth *fakeAuth) http.Handler {
	return New(Deps{
		Querier: &fakeQuerier{}, Dashboards: fakeDashboards{}, Auth: auth, Users: fakeUsers{},
		SecureCookies: true, SessionTTL: time.Hour, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func call(h http.Handler, method, path, token, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoleGuards(t *testing.T) {
	h := authed(&fakeAuth{})
	cases := []struct {
		method, path, token string
		want                int
	}{
		{"GET", "/api/v1/services", "", http.StatusUnauthorized},
		{"GET", "/api/v1/services", "bogus", http.StatusUnauthorized},
		{"GET", "/api/v1/services", "viewer", http.StatusOK},
		{"POST", "/api/v1/dashboards", "viewer", http.StatusForbidden},
		{"POST", "/api/v1/dashboards", "editor", http.StatusBadRequest}, // passed the guard
		{"GET", "/api/v1/users", "editor", http.StatusForbidden},
		{"GET", "/api/v1/users", "admin", http.StatusOK},
		{"GET", "/healthz", "", http.StatusOK},
	}
	for _, c := range cases {
		if rec := call(h, c.method, c.path, c.token, "{}"); rec.Code != c.want {
			t.Errorf("%s %s as %q: %d, want %d (%s)", c.method, c.path, c.token, rec.Code, c.want, rec.Body)
		}
	}
}

func TestNoPasswordHashInResponses(t *testing.T) {
	h := authed(&fakeAuth{})
	for _, path := range []string{"/api/v1/users", "/api/v1/auth/me"} {
		if rec := call(h, "GET", path, "admin", ""); strings.Contains(rec.Body.String(), "secret-hash") || strings.Contains(rec.Body.String(), "password") {
			t.Errorf("%s leaks the hash: %s", path, rec.Body)
		}
	}
}

func TestLoginCookie(t *testing.T) {
	rec := call(authed(&fakeAuth{}), "POST", "/api/v1/auth/login", "", `{"username":"editor","password":"x"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	c := rec.Result().Cookies()[0]
	if c.Name != sessionCookie || c.Value != "editor" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 3600 {
		t.Errorf("cookie = %+v", c)
	}

	for err, want := range map[error]int{
		domain.ErrBadCredentials: http.StatusUnauthorized,
		domain.ErrLockedOut:      http.StatusTooManyRequests,
	} {
		rec := call(authed(&fakeAuth{loginErr: err}), "POST", "/api/v1/auth/login", "", `{"username":"a","password":"b"}`)
		if rec.Code != want || len(rec.Result().Cookies()) != 0 {
			t.Errorf("%v: %d, cookies %v", err, rec.Code, rec.Result().Cookies())
		}
	}

	out := call(authed(&fakeAuth{}), "POST", "/api/v1/auth/logout", "viewer", "")
	if out.Code != http.StatusNoContent || out.Result().Cookies()[0].MaxAge >= 0 {
		t.Errorf("logout should clear the cookie: %d %+v", out.Code, out.Result().Cookies())
	}
}

func TestSameOrigin(t *testing.T) {
	h := authed(&fakeAuth{})
	if rec := call(h, "POST", "/api/v1/dashboards", "editor", "{}", "Origin", "https://evil.example"); rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST: %d", rec.Code)
	}
	if rec := call(h, "POST", "/api/v1/auth/login", "", "{}", "Sec-Fetch-Site", "cross-site"); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site login: %d", rec.Code)
	}
	if rec := call(h, "POST", "/api/v1/dashboards", "editor", "{}", "Origin", "http://example.com"); rec.Code == http.StatusForbidden {
		t.Errorf("same-origin POST refused (httptest host is example.com)")
	}
	if rec := call(h, "GET", "/api/v1/services", "viewer", "", "Origin", "https://evil.example"); rec.Code != http.StatusOK {
		t.Errorf("cross-origin GET should pass (reads are not CSRF targets): %d", rec.Code)
	}
}
