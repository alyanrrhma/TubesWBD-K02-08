package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	h, e := HashPassword("correct-horse-battery")
	if e != nil {
		t.Fatal(e)
	}
	if !VerifyPassword(h, "correct-horse-battery") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(h, "wrong") {
		t.Fatal("invalid password accepted")
	}
}
func setup(t *testing.T) (*Manager, *MemoryStore, *time.Time) {
	t.Helper()
	s := NewMemoryStore()
	c := DefaultConfig()
	c.SecureCookies = false
	c.IdleTimeout = 5 * time.Minute
	c.AbsoluteTimeout = time.Hour
	c.RotationInterval = 3 * time.Minute
	c.GracePeriod = 10 * time.Second
	m, e := NewManager(s, c)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	m.Now = func() time.Time { return now }
	return m, s, &now
}
func login(t *testing.T, m *Manager) (*http.Cookie, string) {
	t.Helper()
	w := httptest.NewRecorder()
	csrf, e := m.Login(context.Background(), w, Principal{UserID: 7})
	if e != nil {
		t.Fatal(e)
	}
	return w.Result().Cookies()[0], csrf
}
func request(c *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", "http://localhost/protected", nil)
	r.AddCookie(c)
	return r
}
func TestLifecycleAndCSRF(t *testing.T) {
	m, _, clock := setup(t)
	cookie, csrf := login(t, m)
	w := httptest.NewRecorder()
	s, _, e := m.Validate(context.Background(), w, request(cookie))
	if e != nil {
		t.Fatal(e)
	}
	if CheckCSRF(s, csrf) != nil {
		t.Fatal("csrf invalid")
	}
	if CheckCSRF(s, "invalid") == nil {
		t.Fatal("csrf accepted")
	}
	*clock = clock.Add(4 * time.Minute)
	rot := httptest.NewRecorder()
	_, _, e = m.Validate(context.Background(), rot, request(cookie))
	if e != nil {
		t.Fatal(e)
	}
	newCookie := rot.Result().Cookies()[0]
	if newCookie.Value == cookie.Value {
		t.Fatal("not rotated")
	}
	if _, _, e = m.Validate(context.Background(), httptest.NewRecorder(), request(cookie)); e != nil {
		t.Fatal("grace token rejected")
	}
	*clock = clock.Add(11 * time.Second)
	if _, _, e = m.Validate(context.Background(), httptest.NewRecorder(), request(cookie)); e == nil {
		t.Fatal("old token survived grace")
	}
	s, _, e = m.Validate(context.Background(), httptest.NewRecorder(), request(newCookie))
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Logout(context.Background(), httptest.NewRecorder(), s); e != nil {
		t.Fatal(e)
	}
	if _, _, e = m.Validate(context.Background(), httptest.NewRecorder(), request(newCookie)); e == nil {
		t.Fatal("logout did not revoke")
	}
}
func TestAbsoluteExpiryAndIdle(t *testing.T) {
	m, _, clock := setup(t)
	cookie, _ := login(t, m)
	*clock = clock.Add(5 * time.Minute)
	if _, _, e := m.Validate(context.Background(), httptest.NewRecorder(), request(cookie)); e == nil {
		t.Fatal("idle timeout ignored")
	}
	m, _, clock = setup(t)
	cookie, _ = login(t, m)
	for i := 0; i < 12; i++ {
		*clock = clock.Add(4 * time.Minute)
		r := httptest.NewRecorder()
		_, _, e := m.Validate(context.Background(), r, request(cookie))
		if e != nil {
			t.Fatal(e)
		}
		if len(r.Result().Cookies()) > 0 {
			cookie = r.Result().Cookies()[0]
		}
	}
	*clock = clock.Add(13 * time.Minute)
	if _, _, e := m.Validate(context.Background(), httptest.NewRecorder(), request(cookie)); e == nil {
		t.Fatal("absolute expiry ignored")
	}
}
func TestCSRFGuard(t *testing.T) {
	m, _, _ := setup(t)
	cookie, csrf := login(t, m)
	h := m.Authenticate(CSRFGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })))
	for _, tc := range []struct {
		token    string
		expected int
	}{{"", 403}, {csrf, 204}} {
		{
			r := httptest.NewRequest("POST", "http://localhost/action", nil)
			r.AddCookie(cookie)
			r.Header.Set("X-CSRF-Token", tc.token)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.expected {
				t.Fatalf("got %d want %d", w.Code, tc.expected)
			}
		}
	}
}
