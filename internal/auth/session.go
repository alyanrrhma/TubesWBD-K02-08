package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrCSRF            = errors.New("invalid csrf token")
	ErrSessionConflict = errors.New("session changed concurrently")
)

const CookieName = "dbmp_session"

type Config struct {
	IdleTimeout, AbsoluteTimeout, RotationInterval, GracePeriod time.Duration
	SecureCookies                                               bool
}

func DefaultConfig() Config {
	return Config{IdleTimeout: 30 * time.Minute, AbsoluteTimeout: 8 * time.Hour, RotationInterval: 15 * time.Minute, GracePeriod: 30 * time.Second, SecureCookies: true}
}
func (c Config) Validate() error {
	if c.IdleTimeout <= 0 || c.AbsoluteTimeout <= 0 || c.RotationInterval <= 0 || c.GracePeriod <= 0 || c.GracePeriod >= c.RotationInterval {
		return errors.New("invalid session configuration")
	}
	return nil
}

type Principal struct {
	UserID int64
	Admin  bool
}
type Session struct {
	Principal                                 Principal
	FamilyID                                  string
	TokenHash                                 [32]byte
	CSRFHash                                  [32]byte
	CreatedAt, LastSeen, RotatedAt, ExpiresAt time.Time
	Revoked                                   bool
	OldTokenHash                              [32]byte
	OldExpiresAt                              time.Time
}
type Store interface {
	Create(context.Context, Session) error
	FindByTokenHash(context.Context, [32]byte) (Session, error)
	Save(context.Context, Session) error
	CompareAndSwap(context.Context, Session, [32]byte) error
	RevokeFamily(context.Context, string) error
	RevokeUser(context.Context, int64) error
}
type Manager struct {
	Store  Store
	Config Config
	Now    func() time.Time
	mu     sync.Mutex
}

func NewManager(s Store, c Config) (*Manager, error) {
	if s == nil {
		return nil, errors.New("nil session store")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Manager{Store: s, Config: c, Now: time.Now}, nil
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func digest(token string) [32]byte { return sha256.Sum256([]byte(token)) }
func (m *Manager) cookie(w http.ResponseWriter, token string, exp time.Time) {
	maxAge := int(exp.Sub(m.Now()).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.Config.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
		MaxAge:   maxAge,
	})
}
func (m *Manager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: m.Config.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func (m *Manager) Login(ctx context.Context, w http.ResponseWriter, p Principal) (string, error) {
	token, e := randomToken()
	if e != nil {
		return "", e
	}
	csrf, e := randomToken()
	if e != nil {
		return "", e
	}
	family, e := randomToken()
	if e != nil {
		return "", e
	}
	now := m.Now()
	s := Session{Principal: p, FamilyID: family, TokenHash: digest(token), CSRFHash: digest(csrf), CreatedAt: now, LastSeen: now, RotatedAt: now, ExpiresAt: now.Add(m.Config.AbsoluteTimeout)}
	if e := m.Store.Create(ctx, s); e != nil {
		return "", e
	}
	m.cookie(w, token, s.ExpiresAt)
	return csrf, nil
}

// Validate keeps absolute expiry fixed; rotation replaces token and retains old token briefly.
// Callers must persist the returned CSRF value (e.g. in a hidden form input / response body).
func (m *Manager) Validate(ctx context.Context, w http.ResponseWriter, r *http.Request) (Session, string, error) {
	c, e := r.Cookie(CookieName)
	if e != nil || len(c.Value) < 32 || len(c.Value) > 128 {
		return Session{}, "", ErrUnauthenticated
	}
	h := digest(c.Value)
	m.mu.Lock()
	defer m.mu.Unlock() // serializes rotation within one process; DB transactions required for multi-instance deployments
	s, e := m.Store.FindByTokenHash(ctx, h)
	if e != nil {
		return Session{}, "", ErrUnauthenticated
	}
	now := m.Now()
	expectedTokenHash := s.TokenHash
	if s.Revoked || !now.Before(s.ExpiresAt) || now.Sub(s.LastSeen) >= m.Config.IdleTimeout {
		_ = m.Store.RevokeFamily(ctx, s.FamilyID)
		return Session{}, "", ErrUnauthenticated
	}
	oldMatch := subtle.ConstantTimeCompare(h[:], s.OldTokenHash[:]) == 1
	if oldMatch && !now.Before(s.OldExpiresAt) {
		return Session{}, "", ErrUnauthenticated
	}
	if !oldMatch && subtle.ConstantTimeCompare(h[:], s.TokenHash[:]) != 1 {
		return Session{}, "", ErrUnauthenticated
	}
	// Old token continues for grace period only; do not rotate it again.
	if !oldMatch && now.Sub(s.RotatedAt) >= m.Config.RotationInterval {
		next, e := randomToken()
		if e != nil {
			return Session{}, "", e
		}
		s.OldTokenHash = s.TokenHash
		s.OldExpiresAt = now.Add(m.Config.GracePeriod)
		s.TokenHash = digest(next)
		s.RotatedAt = now
		m.cookie(w, next, s.ExpiresAt)
	}
	s.LastSeen = now
	if e = m.Store.CompareAndSwap(ctx, s, expectedTokenHash); e != nil {
		return Session{}, "", e
	}
	return s, "", nil
}
func (m *Manager) Logout(ctx context.Context, w http.ResponseWriter, s Session) error {
	e := m.Store.RevokeFamily(ctx, s.FamilyID)
	m.ClearCookie(w)
	return e
}
func (m *Manager) RevokeUser(ctx context.Context, id int64) error { return m.Store.RevokeUser(ctx, id) }

// CSRF token is minted and returned on successful login; clients can request new one via IssueCSRF.
func (m *Manager) IssueCSRF(ctx context.Context, s Session) (string, error) {
	v, e := randomToken()
	if e != nil {
		return "", e
	}
	expectedTokenHash := s.TokenHash
	s.CSRFHash = digest(v)
	if err := m.Store.CompareAndSwap(ctx, s, expectedTokenHash); err != nil {
		return "", err
	}
	return v, nil
}
func CheckCSRF(s Session, token string) error {
	if len(token) < 32 || len(token) > 128 {
		return ErrCSRF
	}
	h := digest(token)
	if subtle.ConstantTimeCompare(h[:], s.CSRFHash[:]) != 1 {
		return ErrCSRF
	}
	return nil
}

// RotatePrivilege must be called immediately after any privilege elevation.
func (m *Manager) RotatePrivilege(ctx context.Context, w http.ResponseWriter, s Session, p Principal) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tok, e := randomToken()
	if e != nil {
		return "", e
	}
	csrf, e := randomToken()
	if e != nil {
		return "", e
	}
	now := m.Now()
	expectedTokenHash := s.TokenHash
	s.Principal = p
	s.OldTokenHash = s.TokenHash
	s.OldExpiresAt = now.Add(m.Config.GracePeriod)
	s.TokenHash = digest(tok)
	s.CSRFHash = digest(csrf)
	s.RotatedAt = now
	if e = m.Store.CompareAndSwap(ctx, s, expectedTokenHash); e != nil {
		return "", e
	}
	m.cookie(w, tok, s.ExpiresAt)
	return csrf, nil
}