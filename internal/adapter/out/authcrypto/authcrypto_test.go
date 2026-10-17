package authcrypto

import (
	"strings"
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

func TestBcrypt(t *testing.T) {
	b := Bcrypt{Cost: 4}
	h, err := b.Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Verify(h, "correct horse") || b.Verify(h, "wrong horse") || b.Verify("not-a-hash", "x") {
		t.Error("verify results wrong")
	}
}

func TestHMACRoundTripAndTamper(t *testing.T) {
	c, err := NewHMAC([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_000_000, 0)
	tok := c.Encode(domain.Session{UserID: "u1", Version: 3, Expires: now.Add(time.Hour)})

	s, err := c.Decode(tok, now)
	if err != nil || s.UserID != "u1" || s.Version != 3 {
		t.Fatalf("decode: %+v %v", s, err)
	}
	if _, err := c.Decode(tok, now.Add(2*time.Hour)); err == nil {
		t.Error("expired token accepted")
	}

	other, _ := NewHMAC([]byte(strings.Repeat("x", 32)))
	if _, err := other.Decode(tok, now); err == nil {
		t.Error("token signed with another key accepted")
	}

	// Swap in a payload claiming version 4, keeping the old signature.
	forged := c.Encode(domain.Session{UserID: "u1", Version: 4, Expires: now.Add(time.Hour)})
	p, _, _ := strings.Cut(forged, ".")
	_, sig, _ := strings.Cut(tok, ".")
	if _, err := c.Decode(p+"."+sig, now); err == nil {
		t.Error("forged payload accepted")
	}
	for _, bad := range []string{"", "abc", "a.b", tok + "x"} {
		if _, err := c.Decode(bad, now); err == nil {
			t.Errorf("garbage %q accepted", bad)
		}
	}
	if _, err := NewHMAC([]byte("short")); err == nil {
		t.Error("short key accepted")
	}
}
