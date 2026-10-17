package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

var (
	ErrUnauthenticated = errors.New("not signed in")
	ErrForbidden       = errors.New("not allowed")
	ErrBadCredentials  = errors.New("wrong username or password")
	// ErrLockedOut means too many failed sign-ins; try again later.
	ErrLockedOut = errors.New("too many failed sign-ins, try again later")
)

// Role grants a set of permissions; each role includes the ones below it.
type Role string

const (
	RoleViewer Role = "viewer" // read everything
	RoleEditor Role = "editor" // + change dashboards and alerts
	RoleAdmin  Role = "admin"  // + manage users
)

var roleRank = map[Role]int{RoleViewer: 1, RoleEditor: 2, RoleAdmin: 3}

func (r Role) Valid() bool { return roleRank[r] > 0 }

// Allows reports whether r includes everything need does.
func (r Role) Allows(need Role) bool { return roleRank[r] >= roleRank[need] }

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	Role         Role   `json:"role"`
	PasswordHash string `json:"password_hash"`
	Disabled     bool   `json:"disabled"`
	// SessionVersion is part of every session; bumping it signs the user
	// out everywhere (password change, sign-out, disable).
	SessionVersion int       `json:"session_version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9._@-]{2,64}$`)

func ValidateUsername(u string) error {
	if !usernameRe.MatchString(u) {
		return fmt.Errorf("%w: username must be 2-64 letters, digits or . _ @ -", ErrInvalidQuery)
	}
	return nil
}

const MinPasswordLen = 10

func ValidatePassword(p string) error {
	if len(p) < MinPasswordLen {
		return fmt.Errorf("%w: password must be at least %d characters", ErrInvalidQuery, MinPasswordLen)
	}
	if len(p) > 72 { // bcrypt ignores anything longer
		return fmt.Errorf("%w: password must be at most 72 bytes", ErrInvalidQuery)
	}
	return nil
}

// Session is what a signed session cookie carries.
type Session struct {
	UserID  string
	Version int
	Expires time.Time
}
