package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidKey rejects ingest without a valid API key (OTLP: HTTP 401 /
// gRPC Unauthenticated, which clients do not retry).
var ErrInvalidKey = errors.New("missing or invalid API key")

// KeyPrefix starts every ingest key, so leaked keys are easy to recognise
// in logs and secret scanners.
const KeyPrefix = "vdc_"

// APIKey authorises sending telemetry. Only a SHA-256 of the secret is
// stored; keys are long random strings, so a fast hash is enough.
type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Hash      string    `json:"hash"`
	Hint      string    `json:"hint"` // first characters, to tell keys apart
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	Revoked   bool      `json:"revoked"`
	RevokedAt time.Time `json:"revoked_at,omitzero"`
}

func HashKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func ValidateKeyName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 100 {
		return fmt.Errorf("%w: key name must be 1-100 characters", ErrInvalidQuery)
	}
	return nil
}
