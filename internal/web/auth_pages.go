package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
)

type loginData struct {
	Username string
	Next     string
	Error    string
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if _, hasCookie := cookieValue(r, auth.CookieName); hasCookie {
		if _, _, err := s.Sessions.Validate(r.Context(), w, r); err == nil {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
	}
	s.render(w, r, http.StatusOK, "login", "Masuk", "", loginData{Next: safeNext(r.URL.Query().Get("next"))})
}

// dummyHash dipakai untuk menyamakan waktu respons saat username tidak ada
var dummyHash = sync.OnceValue(func() string {
	h, _ := auth.HashPassword("dummy-password-for-timing")
	return h
})

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); o != "" && !sameOrigin(o, r.Host) {
		s.errorPage(w, r, http.StatusForbidden, "Permintaan ditolak", "Permintaan berasal dari situs lain.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		s.errorPage(w, r, http.StatusRequestEntityTooLarge, "Data terlalu besar", "Data yang dikirim terlalu besar.")
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	user, err := s.Users.FindByUsername(r.Context(), username)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, err)
		return
	}
	hash := dummyHash()
	if found {
		hash = user.PasswordHash
	}
	if !auth.VerifyPassword(hash, password) || !found || username == "" || password == "" {
		s.render(w, r, http.StatusUnauthorized, "login", "Masuk", "", loginData{
			Username: username, Next: next, Error: "Username atau password salah.",
		})
		return
	}

	// Session lama dicabut setelah autentikasi berhasil (mencegah session fixation)
	auth.LoginService{Users: s.Users, Sessions: s.Sessions}.RevokeCurrent(r.Context(), w, r)
	csrf, err := s.Sessions.Login(r.Context(), w, auth.Principal{UserID: user.ID, Admin: user.Admin})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.setCSRFCookie(w, csrf)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.Sessions.Logout(r.Context(), w, reqFrom(r.Context()).Session); err != nil {
		s.Logger.Error("logout gagal", "err", err)
	}
	s.endSession(w)
	s.setFlash(w, "success", "Anda telah keluar.")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	dbs, err := s.Viewer.ListDatabases(r.Context(), currentUser(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "dashboard", "Database", "dashboard", dbs)
}