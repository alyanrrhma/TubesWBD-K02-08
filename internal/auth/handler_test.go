package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type fakeUserStore struct{ user User }

func (f fakeUserStore) FindByUsername(_ context.Context, name string) (User, error) {
	if name != "student" {
		return User{}, ErrCredentials
	}
	return f.user, nil
}
func TestLoginHandlerAndLogout(t *testing.T) {
	m, _, _ := setup(t)
	hash, e := HashPassword("student-password")
	if e != nil {
		t.Fatal(e)
	}
	h := Handler{Sessions: m, LoginService: LoginService{Users: fakeUserStore{User{ID: 5, PasswordHash: hash}}, Sessions: m}}
	form := url.Values{"username": {"student"}, "password": {"student-password"}}
	r := httptest.NewRequest("POST", "http://localhost/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.Login(w, r)
	if w.Code != 200 {
		t.Fatalf("login status=%d: %s", w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if cookie.HttpOnly != true {
		t.Fatal("not httponly")
	}
	me := m.Authenticate(http.HandlerFunc(h.Me))
	get := httptest.NewRequest("GET", "http://localhost/me", nil)
	get.AddCookie(cookie)
	result := httptest.NewRecorder()
	me.ServeHTTP(result, get)
	if result.Code != 200 {
		t.Fatal("/me unauthorized")
	}
	csrf := strings.Split(strings.Split(w.Body.String(), `"csrf_token":"`)[1], `"`)[0]
	logout := m.Authenticate(CSRFGuard(http.HandlerFunc(h.Logout)))
	post := httptest.NewRequest("POST", "http://localhost/logout", nil)
	post.Header.Set("X-CSRF-Token", csrf)
	post.AddCookie(cookie)
	out := httptest.NewRecorder()
	logout.ServeHTTP(out, post)
	if out.Code != 200 {
		t.Fatalf("logout: %d", out.Code)
	}
	result = httptest.NewRecorder()
	me.ServeHTTP(result, get)
	if result.Code != 401 {
		t.Fatal("logout did not invalidate cookie")
	}
}
func TestLoginBadCredentials(t *testing.T) {
	m, _, _ := setup(t)
	hash, _ := HashPassword("student-password")
	h := Handler{Sessions: m, LoginService: LoginService{Users: fakeUserStore{User{ID: 5, PasswordHash: hash}}, Sessions: m}}
	r := httptest.NewRequest("POST", "http://localhost/login", strings.NewReader("username=student&password=bad"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.Login(w, r)
	if w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatal("invalid password accepted")
	}
	r = httptest.NewRequest("POST", "http://localhost/login", strings.NewReader("username=student&password=student-password"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	h.Login(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("origin mismatch accepted: %d", w.Code)
	}
}