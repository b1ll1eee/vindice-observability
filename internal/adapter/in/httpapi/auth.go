package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

const sessionCookie = "vindice_session"

type cookieConfig struct {
	secure bool
	ttl    time.Duration
}

type userKey struct{}

func currentUser(r *http.Request) (domain.User, bool) {
	u, ok := r.Context().Value(userKey{}).(domain.User)
	return u, ok
}

// guard resolves the session cookie and checks the route's least role.
func (h *Handler) guard(need domain.Role, fn http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.auth == nil {
			fn(w, r)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			h.fail(w, domain.ErrUnauthenticated)
			return
		}
		u, err := h.auth.Authenticate(r.Context(), c.Value)
		if err != nil {
			h.fail(w, err)
			return
		}
		if !u.Role.Allows(need) {
			h.fail(w, domain.ErrForbidden)
			return
		}
		fn(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	})
}

// sameOrigin rejects state-changing requests sent by other sites (CSRF).
// The session cookie is SameSite=Lax as well; this check also covers
// browsers or setups where that is not enough.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, http.StatusForbidden, "cross-site request refused")
				return
			}
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					writeError(w, http.StatusForbidden, "cross-origin request refused")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

type userDTO struct {
	ID        string      `json:"id"`
	Username  string      `json:"username"`
	Role      domain.Role `json:"role"`
	Disabled  bool        `json:"disabled"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// toUserDTO never includes the password hash.
func toUserDTO(u domain.User) userDTO {
	return userDTO{ID: u.ID, Username: u.Username, Role: u.Role, Disabled: u.Disabled, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
}

func (h *Handler) setSession(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: h.cookie.secure, SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &body); err != nil {
		h.fail(w, err)
		return
	}
	// RemoteAddr, not X-Forwarded-For, which any client can set.
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	token, u, err := h.auth.Login(r.Context(), body.Username, body.Password, ip)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.setSession(w, token, int(h.cookie.ttl/time.Second))
	writeJSON(w, toUserDTO(u))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	if err := h.auth.Logout(r.Context(), u.ID); err != nil {
		h.fail(w, err)
		return
	}
	h.setSession(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	writeJSON(w, toUserDTO(u))
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := decode(w, r, &body); err != nil {
		h.fail(w, err)
		return
	}
	u, _ := currentUser(r)
	if err := h.auth.ChangePassword(r.Context(), u.ID, body.Current, body.New); err != nil {
		h.fail(w, err)
		return
	}
	// The change ended every session, this one included; start a new one.
	token, _, err := h.auth.Login(r.Context(), u.Username, body.New, "")
	if err == nil {
		h.setSession(w, token, int(h.cookie.ttl/time.Second))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.ListUsers(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]userDTO, 0, len(users))
	for _, u := range users {
		out = append(out, toUserDTO(u))
	}
	writeJSON(w, out)
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string      `json:"username"`
		Role     domain.Role `json:"role"`
		Password string      `json:"password"`
	}
	if err := decode(w, r, &body); err != nil {
		h.fail(w, err)
		return
	}
	u, err := h.users.CreateUser(r.Context(), body.Username, body.Role, body.Password)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, toUserDTO(u))
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Role     domain.Role `json:"role"`
		Disabled bool        `json:"disabled"`
		Password string      `json:"password,omitempty"`
	}
	if err := decode(w, r, &body); err != nil {
		h.fail(w, err)
		return
	}
	actor, _ := currentUser(r)
	u, err := h.users.UpdateUser(r.Context(), actor.ID, r.PathValue("id"), body.Role, body.Disabled, body.Password)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, toUserDTO(u))
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := currentUser(r)
	if err := h.users.DeleteUser(r.Context(), actor.ID, r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
