package auth

import (
	"context"
	"net/http"
	"net/url"
)

type sessionKey struct{}

func SessionFromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(Session)
	return s, ok
}
func (m *Manager) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _, e := m.Validate(r.Context(), w, r)
		if e != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, s)))
	})
}

// Apply CSRF guard after Authenticate. Token comes from X-CSRF-Token or csrf_token form field.
// Also rejects cross-origin unsafe requests using Origin/Referer against request host.
func CSRFGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		s, ok := SessionFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && !originMatchesHost(origin, r.Host) {
			http.Error(w, "csrf rejected", http.StatusForbidden)
			return
		}
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			token = r.PostFormValue("csrf_token")
		}
		if CheckCSRF(s, token) != nil {
			http.Error(w, "csrf rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return (u.Scheme == "https" || u.Scheme == "http") && u.Host == host && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}