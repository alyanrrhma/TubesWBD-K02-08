package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newRegressionManager(t *testing.T, cfg Config) (*Manager, *MemoryStore, func(time.Duration)) {
	t.Helper()

	store := NewMemoryStore()
	manager, err := NewManager(store, cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	now := time.Now()
	manager.Now = func() time.Time {
		return now
	}

	advance := func(d time.Duration) {
		now = now.Add(d)
	}

	return manager, store, advance
}

func loginForRegressionTest(t *testing.T, manager *Manager) (*http.Cookie, string) {
	t.Helper()

	recorder := httptest.NewRecorder()
	csrf, err := manager.Login(
		context.Background(),
		recorder,
		Principal{UserID: 1},
	)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	cookies := recorder.Result().Cookies()
	for _, cookie := range cookies {
		if cookie.Name == CookieName {
			return cookie, csrf
		}
	}

	t.Fatal("Login() did not set the session cookie")
	return nil, ""
}

func validateForRegressionTest(
	manager *Manager,
	cookie *http.Cookie,
) (Session, error) {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(cookie)

	recorder := httptest.NewRecorder()
	session, _, err := manager.Validate(
		context.Background(),
		recorder,
		req,
	)
	return session, err
}

func TestRegressionIdleTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleTimeout = time.Minute
	cfg.AbsoluteTimeout = 8 * time.Hour

	manager, _, advance := newRegressionManager(t, cfg)
	cookie, _ := loginForRegressionTest(t, manager)

	advance(cfg.IdleTimeout + time.Second)

	_, err := validateForRegressionTest(manager, cookie)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated after idle timeout, got %v", err)
	}
}

func TestRegressionAbsoluteTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdleTimeout = 2 * time.Hour
	cfg.AbsoluteTimeout = time.Hour

	manager, _, advance := newRegressionManager(t, cfg)
	cookie, _ := loginForRegressionTest(t, manager)

	advance(cfg.AbsoluteTimeout + time.Second)

	_, err := validateForRegressionTest(manager, cookie)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated after absolute timeout, got %v", err)
	}
}

func TestRegressionTokenRotationAndGracePeriod(t *testing.T) {
	cfg := DefaultConfig()

	manager, _, advance := newRegressionManager(t, cfg)
	oldCookie, _ := loginForRegressionTest(t, manager)

	// Move beyond the rotation interval.
	advance(cfg.RotationInterval + time.Second)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(oldCookie)
	recorder := httptest.NewRecorder()

	_, _, err := manager.Validate(context.Background(), recorder, req)
	if err != nil {
		t.Fatalf("Validate() during rotation error = %v", err)
	}

	var newCookie *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == CookieName {
			newCookie = cookie
			break
		}
	}

	if newCookie == nil {
		t.Fatal("expected a replacement session cookie after rotation")
	}
	if newCookie.Value == oldCookie.Value {
		t.Fatal("session token did not change after rotation")
	}

	// The old token should still work during the grace period.
	_, err = validateForRegressionTest(manager, oldCookie)
	if err != nil {
		t.Fatalf("old token should work during grace period, got %v", err)
	}

	// Once the grace period has passed, the old token must be rejected.
	advance(cfg.GracePeriod + time.Second)

	_, err = validateForRegressionTest(manager, oldCookie)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected old token to be rejected after grace period, got %v", err)
	}
}

func TestRegressionLogoutRevokesSession(t *testing.T) {
	cfg := DefaultConfig()

	manager, _, _ := newRegressionManager(t, cfg)
	cookie, _ := loginForRegressionTest(t, manager)

	session, err := validateForRegressionTest(manager, cookie)
	if err != nil {
		t.Fatalf("Validate() before logout error = %v", err)
	}

	recorder := httptest.NewRecorder()
	if err := manager.Logout(context.Background(), recorder, session); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	_, err = validateForRegressionTest(manager, cookie)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected revoked session to be rejected, got %v", err)
	}
}

func TestRegressionPrivilegeRotation(t *testing.T) {
	cfg := DefaultConfig()
	manager, _, advance := newRegressionManager(t, cfg)

	// Login sebagai user biasa.
	oldCookie, oldCSRF := loginForRegressionTest(t, manager)

	// Ambil session aktif sebelum privilege berubah.
	session, err := validateForRegressionTest(manager, oldCookie)
	if err != nil {
		t.Fatalf("Validate() before privilege rotation: %v", err)
	}
	if session.Principal.Admin {
		t.Fatal("expected initial session to be non-admin")
	}

	// Naikkan privilege dan rotasi token.
	recorder := httptest.NewRecorder()
	newCSRF, err := manager.RotatePrivilege(
		context.Background(),
		recorder,
		session,
		Principal{UserID: session.Principal.UserID, Admin: true},
	)
	if err != nil {
		t.Fatalf("RotatePrivilege() error = %v", err)
	}

	// Pastikan server mengirim cookie session baru.
	var newCookie *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == CookieName {
			newCookie = cookie
			break
		}
	}
	if newCookie == nil {
		t.Fatal("expected a new session cookie after privilege rotation")
	}
	if newCookie.Value == oldCookie.Value {
		t.Fatal("session token did not change after privilege rotation")
	}
	if newCSRF == "" || newCSRF == oldCSRF {
		t.Fatal("expected a new CSRF token after privilege rotation")
	}

	// Session baru harus mencerminkan privilege terbaru.
	elevatedSession, err := validateForRegressionTest(manager, newCookie)
	if err != nil {
		t.Fatalf("Validate() after privilege rotation: %v", err)
	}
	if !elevatedSession.Principal.Admin {
		t.Fatal("expected session to have admin privilege after rotation")
	}

	// CSRF token lama harus tidak berlaku lagi.
	if err := CheckCSRF(elevatedSession, oldCSRF); !errors.Is(err, ErrCSRF) {
		t.Fatalf("expected old CSRF token to be rejected, got %v", err)
	}
	if err := CheckCSRF(elevatedSession, newCSRF); err != nil {
		t.Fatalf("expected new CSRF token to be accepted, got %v", err)
	}

	// Token session lama masih boleh digunakan selama grace period.
	if _, err := validateForRegressionTest(manager, oldCookie); err != nil {
		t.Fatalf("old session token should work during grace period, got %v", err)
	}

	// Setelah grace period, token lama harus ditolak.
	advance(cfg.GracePeriod + time.Second)
	if _, err := validateForRegressionTest(manager, oldCookie); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected old session token to be rejected after grace period, got %v", err)
	}
}

func TestRegressionRevokeUserRevokesAllSessions(t *testing.T) {
	cfg := DefaultConfig()
	manager, _, _ := newRegressionManager(t, cfg)

	// Login dua kali sebagai user yang sama, menghasilkan dua session.
	login := func(userID int64) *http.Cookie {
		t.Helper()

		recorder := httptest.NewRecorder()
		_, err := manager.Login(
			context.Background(),
			recorder,
			Principal{UserID: userID},
		)
		if err != nil {
			t.Fatalf("Login(userID=%d) error = %v", userID, err)
		}

		for _, cookie := range recorder.Result().Cookies() {
			if cookie.Name == CookieName {
				return cookie
			}
		}

		t.Fatalf("Login(userID=%d) did not set session cookie", userID)
		return nil
	}

	user1CookieA := login(1)
	user1CookieB := login(1)
	user2Cookie := login(2)

	// Pastikan semua session valid sebelum revocation.
	for name, cookie := range map[string]*http.Cookie{
		"user 1 session A": user1CookieA,
		"user 1 session B": user1CookieB,
		"user 2 session":   user2Cookie,
	} {
		if _, err := validateForRegressionTest(manager, cookie); err != nil {
			t.Fatalf("%s should be valid before revocation, got %v", name, err)
		}
	}

	// Cabut semua session milik user 1.
	if err := manager.RevokeUser(context.Background(), 1); err != nil {
		t.Fatalf("RevokeUser(1) error = %v", err)
	}

	// Kedua session user 1 harus ditolak.
	for name, cookie := range map[string]*http.Cookie{
		"user 1 session A": user1CookieA,
		"user 1 session B": user1CookieB,
	} {
		if _, err := validateForRegressionTest(manager, cookie); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("%s should be rejected after revocation, got %v", name, err)
		}
	}

	// Session milik user 2 tidak boleh ikut tercabut.
	if _, err := validateForRegressionTest(manager, user2Cookie); err != nil {
		t.Errorf("user 2 session should remain valid, got %v", err)
	}
}
