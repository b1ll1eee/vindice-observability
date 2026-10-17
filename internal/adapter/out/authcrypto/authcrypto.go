// Package authcrypto implements password hashing (bcrypt) and signed
// session tokens (HMAC-SHA256).
package authcrypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
	"github.com/b1ll1eee/vindice-observability/internal/core/port"
)

type Bcrypt struct{ Cost int }

var _ port.PasswordHasher = Bcrypt{}

func (b Bcrypt) Hash(password string) (string, error) {
	cost := b.Cost
	if cost == 0 {
		cost = 12
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	return string(h), err
}

func (Bcrypt) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HMAC encodes a session as "userID.version.expiresUnix" plus an
// HMAC-SHA256 signature, both base64url, joined by ".".
type HMAC struct{ key []byte }

var _ port.SessionCodec = (*HMAC)(nil)

var ErrBadToken = errors.New("invalid session token")

func NewHMAC(key []byte) (*HMAC, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("session key must be at least 32 bytes, got %d", len(key))
	}
	return &HMAC{key: key}, nil
}

func (h *HMAC) Encode(s domain.Session) string {
	payload := fmt.Sprintf("%s.%d.%d", s.UserID, s.Version, s.Expires.Unix())
	p := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return p + "." + base64.RawURLEncoding.EncodeToString(h.sign(p))
}

func (h *HMAC) Decode(token string, now time.Time) (domain.Session, error) {
	p, sig, ok := strings.Cut(token, ".")
	if !ok {
		return domain.Session{}, ErrBadToken
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, h.sign(p)) {
		return domain.Session{}, ErrBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return domain.Session{}, ErrBadToken
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 3 {
		return domain.Session{}, ErrBadToken
	}
	ver, err1 := strconv.Atoi(parts[1])
	exp, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil {
		return domain.Session{}, ErrBadToken
	}
	s := domain.Session{UserID: parts[0], Version: ver, Expires: time.Unix(exp, 0)}
	if !now.Before(s.Expires) {
		return domain.Session{}, ErrBadToken
	}
	return s, nil
}

func (h *HMAC) sign(payload string) []byte {
	m := hmac.New(sha256.New, h.key)
	m.Write([]byte(payload))
	return m.Sum(nil)
}
