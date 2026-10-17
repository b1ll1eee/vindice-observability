package httpapi

import (
	"net/http"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

type keyDTO struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Hint      string     `json:"hint"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	Revoked   bool       `json:"revoked"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}

// toKeyDTO never includes the hash.
func toKeyDTO(k domain.APIKey, lastUsed time.Time) keyDTO {
	d := keyDTO{ID: k.ID, Name: k.Name, Hint: k.Hint, CreatedBy: k.CreatedBy, CreatedAt: k.CreatedAt, Revoked: k.Revoked}
	if !k.RevokedAt.IsZero() {
		d.RevokedAt = &k.RevokedAt
	}
	if !lastUsed.IsZero() {
		d.LastUsed = &lastUsed
	}
	return d
}

func (h *Handler) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, used, err := h.keys.ListKeys(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]keyDTO, 0, len(keys))
	for _, k := range keys {
		out = append(out, toKeyDTO(k, used[k.ID]))
	}
	writeJSON(w, out)
}

func (h *Handler) createKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(w, r, &body); err != nil {
		h.fail(w, err)
		return
	}
	actor, _ := currentUser(r)
	k, secret, err := h.keys.CreateKey(r.Context(), body.Name, actor.Username)
	if err != nil {
		h.fail(w, err)
		return
	}
	// The secret is returned this once and never stored or shown again.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, struct {
		keyDTO
		Secret string `json:"secret"`
	}{toKeyDTO(k, time.Time{}), secret})
}

func (h *Handler) revokeKey(w http.ResponseWriter, r *http.Request) {
	if err := h.keys.RevokeKey(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
