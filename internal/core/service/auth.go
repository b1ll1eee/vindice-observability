package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

const (
	maxFailures   = 5
	failureWindow = 15 * time.Minute
	lockout       = 15 * time.Minute
	userCacheTTL  = 30 * time.Second
)

// AuthService signs users in, resolves sessions and manages users.
type AuthService struct {
	users      port.UserRepository
	hasher     port.PasswordHasher
	codec      port.SessionCodec
	sessionTTL time.Duration
	now        func() time.Time

	// dummyHash is verified against when a username does not exist, so a
	// wrong username takes as long as a wrong password.
	dummyHash string

	mu       sync.Mutex
	cache    map[string]cachedUser
	failures map[string]*failures
}

type cachedUser struct {
	user domain.User
	at   time.Time
}

type failures struct {
	count       int
	first       time.Time
	lockedUntil time.Time
}

var (
	_ port.Authenticator = (*AuthService)(nil)
	_ port.UserManager   = (*AuthService)(nil)
)

func NewAuthService(users port.UserRepository, hasher port.PasswordHasher, codec port.SessionCodec, sessionTTL time.Duration) (*AuthService, error) {
	dummy, err := hasher.Hash(randomPassword())
	if err != nil {
		return nil, err
	}
	return &AuthService{
		users: users, hasher: hasher, codec: codec, sessionTTL: sessionTTL, now: time.Now, dummyHash: dummy,
		cache: map[string]cachedUser{}, failures: map[string]*failures{},
	}, nil
}

// Bootstrap creates the first admin when there are no users. With an empty
// password a random one is generated and returned, so it can be shown once.
func (a *AuthService) Bootstrap(ctx context.Context, username, password string) (generated string, err error) {
	users, err := a.users.ListUsers(ctx)
	if err != nil || len(users) > 0 {
		return "", err
	}
	if password == "" {
		password = randomPassword()
		generated = password
	}
	_, err = a.CreateUser(ctx, username, domain.RoleAdmin, password)
	return generated, err
}

func (a *AuthService) Login(ctx context.Context, username, password, clientIP string) (string, domain.User, error) {
	key := strings.ToLower(username) + "|" + clientIP
	now := a.now()
	if a.locked(key, now) {
		return "", domain.User{}, domain.ErrLockedOut
	}

	u, found, err := a.byUsername(ctx, username)
	if err != nil {
		return "", domain.User{}, err
	}
	hash := a.dummyHash
	if found {
		hash = u.PasswordHash
	}
	ok := a.hasher.Verify(hash, password)
	if !found || !ok || u.Disabled {
		if a.fail(key, now) {
			return "", domain.User{}, domain.ErrLockedOut
		}
		return "", domain.User{}, domain.ErrBadCredentials
	}

	a.mu.Lock()
	delete(a.failures, key)
	a.mu.Unlock()
	token := a.codec.Encode(domain.Session{UserID: u.ID, Version: u.SessionVersion, Expires: now.Add(a.sessionTTL)})
	return token, u, nil
}

func (a *AuthService) Authenticate(ctx context.Context, token string) (domain.User, error) {
	s, err := a.codec.Decode(token, a.now())
	if err != nil {
		return domain.User{}, domain.ErrUnauthenticated
	}
	u, ok, err := a.cachedUser(ctx, s.UserID)
	if err != nil {
		return domain.User{}, err
	}
	if !ok || u.Disabled || u.SessionVersion != s.Version {
		return domain.User{}, domain.ErrUnauthenticated
	}
	return u, nil
}

func (a *AuthService) Logout(ctx context.Context, userID string) error {
	u, err := a.get(ctx, userID)
	if err != nil {
		return err
	}
	u.SessionVersion++
	return a.save(ctx, u)
}

func (a *AuthService) ChangePassword(ctx context.Context, userID, current, next string) error {
	u, err := a.get(ctx, userID)
	if err != nil {
		return err
	}
	if !a.hasher.Verify(u.PasswordHash, current) {
		return fmt.Errorf("%w: current password is wrong", domain.ErrInvalidQuery)
	}
	if err := a.setPassword(&u, next); err != nil {
		return err
	}
	return a.save(ctx, u)
}

func (a *AuthService) ListUsers(ctx context.Context) ([]domain.User, error) {
	users, err := a.users.ListUsers(ctx)
	slices.SortFunc(users, func(x, y domain.User) int { return strings.Compare(x.Username, y.Username) })
	return users, err
}

func (a *AuthService) CreateUser(ctx context.Context, username string, role domain.Role, password string) (domain.User, error) {
	if err := domain.ValidateUsername(username); err != nil {
		return domain.User{}, err
	}
	if !role.Valid() {
		return domain.User{}, fmt.Errorf("%w: unknown role %q", domain.ErrInvalidQuery, role)
	}
	if _, taken, err := a.byUsername(ctx, username); err != nil || taken {
		if err == nil {
			err = fmt.Errorf("%w: username %q is taken", domain.ErrInvalidQuery, username)
		}
		return domain.User{}, err
	}
	now := a.now().UTC()
	u := domain.User{ID: newID(), Username: username, Role: role, CreatedAt: now}
	if err := a.setPassword(&u, password); err != nil {
		return domain.User{}, err
	}
	return u, a.save(ctx, u)
}

func (a *AuthService) UpdateUser(ctx context.Context, actorID, id string, role domain.Role, disabled bool, password string) (domain.User, error) {
	if !role.Valid() {
		return domain.User{}, fmt.Errorf("%w: unknown role %q", domain.ErrInvalidQuery, role)
	}
	u, err := a.get(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	losesAdmin := u.Role == domain.RoleAdmin && !u.Disabled && (role != domain.RoleAdmin || disabled)
	if losesAdmin {
		if id == actorID {
			return domain.User{}, fmt.Errorf("%w: you cannot remove your own admin access", domain.ErrInvalidQuery)
		}
		if err := a.keepAnAdmin(ctx, id); err != nil {
			return domain.User{}, err
		}
	}
	if disabled && !u.Disabled {
		u.SessionVersion++ // sign out now
	}
	u.Role, u.Disabled = role, disabled
	if password != "" {
		if err := a.setPassword(&u, password); err != nil {
			return domain.User{}, err
		}
	}
	return u, a.save(ctx, u)
}

func (a *AuthService) DeleteUser(ctx context.Context, actorID, id string) error {
	if id == actorID {
		return fmt.Errorf("%w: you cannot delete yourself", domain.ErrInvalidQuery)
	}
	u, err := a.get(ctx, id)
	if err != nil {
		return err
	}
	if u.Role == domain.RoleAdmin && !u.Disabled {
		if err := a.keepAnAdmin(ctx, id); err != nil {
			return err
		}
	}
	a.forget(id)
	return a.users.DeleteUser(ctx, id)
}

// keepAnAdmin fails if removing user id would leave no active admin.
func (a *AuthService) keepAnAdmin(ctx context.Context, id string) error {
	users, err := a.users.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.ID != id && u.Role == domain.RoleAdmin && !u.Disabled {
			return nil
		}
	}
	return fmt.Errorf("%w: there must be at least one active admin", domain.ErrInvalidQuery)
}

// setPassword hashes a new password and ends existing sessions.
func (a *AuthService) setPassword(u *domain.User, password string) error {
	if err := domain.ValidatePassword(password); err != nil {
		return err
	}
	h, err := a.hasher.Hash(password)
	if err != nil {
		return err
	}
	u.PasswordHash = h
	u.SessionVersion++
	return nil
}

func (a *AuthService) save(ctx context.Context, u domain.User) error {
	u.UpdatedAt = a.now().UTC()
	if err := a.users.SaveUser(ctx, u); err != nil {
		return err
	}
	a.forget(u.ID)
	return nil
}

func (a *AuthService) get(ctx context.Context, id string) (domain.User, error) {
	u, ok, err := a.users.GetUser(ctx, id)
	if err == nil && !ok {
		err = domain.ErrNotFound
	}
	return u, err
}

func (a *AuthService) byUsername(ctx context.Context, username string) (domain.User, bool, error) {
	users, err := a.users.ListUsers(ctx)
	if err != nil {
		return domain.User{}, false, err
	}
	for _, u := range users {
		if strings.EqualFold(u.Username, username) {
			return u, true, nil
		}
	}
	return domain.User{}, false, nil
}

// cachedUser saves a ClickHouse lookup on every request. Changes made by
// another instance apply within userCacheTTL.
func (a *AuthService) cachedUser(ctx context.Context, id string) (domain.User, bool, error) {
	now := a.now()
	a.mu.Lock()
	c, ok := a.cache[id]
	a.mu.Unlock()
	if ok && now.Sub(c.at) < userCacheTTL {
		return c.user, true, nil
	}
	u, found, err := a.users.GetUser(ctx, id)
	if err != nil || !found {
		return u, found, err
	}
	a.mu.Lock()
	a.cache[id] = cachedUser{user: u, at: now}
	a.mu.Unlock()
	return u, true, nil
}

func (a *AuthService) forget(id string) {
	a.mu.Lock()
	delete(a.cache, id)
	a.mu.Unlock()
}

func (a *AuthService) locked(key string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.failures[key]
	return f != nil && now.Before(f.lockedUntil)
}

// fail records a failed sign-in and reports whether the key is now locked.
func (a *AuthService) fail(key string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.failures) > 100_000 { // bound memory under a spray of usernames
		clear(a.failures)
	}
	f := a.failures[key]
	if f == nil || now.Sub(f.first) > failureWindow {
		f = &failures{first: now}
		a.failures[key] = f
	}
	f.count++
	if f.count >= maxFailures {
		f.lockedUntil = now.Add(lockout)
		return true
	}
	return false
}

const passwordAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randomPassword() string {
	b := make([]byte, 20)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(passwordAlphabet))))
		if err != nil {
			panic(errors.Join(errors.New("crypto/rand failed"), err))
		}
		b[i] = passwordAlphabet[n.Int64()]
	}
	return string(b)
}
