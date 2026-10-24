package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

type Handler struct {
	LoginService LoginService
	Sessions     *Manager
}

func JSON(w http.ResponseWriter, status int, obj any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(obj)
}
func (h Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r.Host) {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	user := strings.TrimSpace(r.PostForm.Get("username"))
	password := r.PostForm.Get("password")
	if user == "" || password == "" {
		JSON(w, 401, map[string]string{"error": "invalid credentials"})
		return
	}
	// Existing session is only revoked after successful authentication.
	u, e := h.LoginService.Users.FindByUsername(r.Context(), user)
	if e != nil || !VerifyPassword(u.PasswordHash, password) {
		JSON(w, 401, map[string]string{"error": "invalid credentials"})
		return
	}
	h.LoginService.RevokeCurrent(r.Context(), w, r)
	csrf, e := h.Sessions.Login(r.Context(), w, Principal{UserID: u.ID, Admin: u.Admin})
	if e != nil {
		http.Error(w, "internal error", 500)
		return
	}
	JSON(w, 200, map[string]string{"csrf_token": csrf, "message": "logged in"})
}
func (h Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	s, ok := SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", 401)
		return
	}
	if err := h.Sessions.Logout(r.Context(), w, s); err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	JSON(w, 200, map[string]string{"message": "logged out"})
}
func (h Handler) Me(w http.ResponseWriter, r *http.Request) {
	s, ok := SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", 401)
		return
	}
	JSON(w, 200, map[string]any{"user_id": s.Principal.UserID, "is_platform_admin": s.Principal.Admin})
}
