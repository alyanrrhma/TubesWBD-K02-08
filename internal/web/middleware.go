package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/admin"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
)

// reqCtx adalah konteks autentikasi satu request, diisi requireAuth.
type reqCtx struct {
	Session auth.Session
	User    userInfo
	CSRF    string // token mentah untuk di-embed ke form (lihat catatan di ensureCSRF)
}

type ctxKey struct{}

func reqFrom(ctx context.Context) *reqCtx {
	rc, _ := ctx.Value(ctxKey{}).(*reqCtx)
	return rc
}

// currentUser hanya dipanggil dari handler di belakang requireAuth.
func currentUser(r *http.Request) userInfo { return reqFrom(r.Context()).User }

// global

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/static/") {
			return
		}
		s.Logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.Logger.Error("panic", "value", v, "path", r.URL.Path)
				http.Error(w, "Terjadi kesalahan pada server.", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Tidak ada inline script/style di template, jadi CSP bisa ketat.
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		if s.Config.Session.SecureCookies {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) staticCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Config.IsProduction() {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// auth

const csrfCookie = "dbmp_csrf"

// requireAuth memvalidasi session lewat auth.Manager (P1), memuat akun, dan menyiapkan token CSRF untuk form. Rotation cookie session ditangani Manager.Validate.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadCookie := cookieValue(r, auth.CookieName)

		sess, _, err := s.Sessions.Validate(r.Context(), w, r)
		if errors.Is(err, auth.ErrSessionConflict) {
			// Request paralel merotasi token pada saat yang sama; token lama masih
			// berlaku selama grace period, jadi satu percobaan ulang cukup.
			sess, _, err = s.Sessions.Validate(r.Context(), w, r)
		}
		if err != nil {
			if errors.Is(err, auth.ErrUnauthenticated) {
				s.endSession(w)
				if hadCookie {
					s.setFlash(w, "info", "Sesi Anda telah berakhir. Silakan masuk kembali.")
				}
				s.redirectToLogin(w, r)
				return
			}
			s.fail(w, r, err)
			return
		}

		u, err := s.Admin.GetUser(r.Context(), sess.Principal.UserID)
		if err != nil {
			if errors.Is(err, admin.ErrNotFound) { // akun sudah dihapus
				s.endSession(w)
				s.redirectToLogin(w, r)
				return
			}
			s.fail(w, r, err)
			return
		}

		rc := &reqCtx{Session: sess, User: userInfo{ID: u.ID, Username: u.Username, IsAdmin: sess.Principal.Admin}}
		rc.CSRF = s.ensureCSRF(w, r, sess)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, rc)))
	})
}

// ensureCSRF mengembalikan token CSRF mentah yang valid untuk session ini.
//
// auth.Manager (P1) hanya menyimpan HASH token CSRF dan hanya mengembalikan nilai
// mentahnya saat login (atau IssueCSRF/RotatePrivilege). Untuk halaman HTML yang
// dirender server, token mentah itu disimpan di cookie terpisah (HttpOnly,
// SameSite=Lax) lalu ditanam di setiap form. Pemeriksaan tetap terhadap hash
// milik session di csrfProtect, bukan terhadap cookie, sehingga cookie hanya
// berfungsi sebagai tempat penyimpanan.
func (s *Server) ensureCSRF(w http.ResponseWriter, r *http.Request, sess auth.Session) string {
	if v, ok := cookieValue(r, csrfCookie); ok && auth.CheckCSRF(sess, v) == nil {
		return v
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return "" 
	}
	tok, err := s.Sessions.IssueCSRF(r.Context(), sess)
	if err != nil {
		s.Logger.Error("IssueCSRF gagal", "err", err)
		return ""
	}
	s.setCSRFCookie(w, tok)
	return tok
}

func (s *Server) setCSRFCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, s.cookieBase(csrfCookie, tok, int(s.Config.Session.AbsoluteTimeout.Seconds())))
}

// endSession menghapus cookie sisi klien (session P1 sudah dicabut di sisi server)
func (s *Server) endSession(w http.ResponseWriter) {
	s.Sessions.ClearCookie(w)
	http.SetCookie(w, s.cookieBase(csrfCookie, "", -1))
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin {
			s.errorPage(w, r, http.StatusForbidden, "Akses ditolak", "Halaman ini hanya untuk Platform Admin.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

const maxFormBytes = 1 << 20 

// csrfProtect memeriksa Origin dan token CSRF pada semua method yang mengubah
// state. Token dari form field csrf_token atau header X-CSRF-Token, dicocokkan
// dengan hash di session lewat auth.CheckCSRF (P1).
func (s *Server) csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !sameOrigin(o, r.Host) {
			s.errorPage(w, r, http.StatusForbidden, "Permintaan ditolak", "Permintaan berasal dari situs lain.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			s.errorPage(w, r, http.StatusRequestEntityTooLarge, "Data terlalu besar", "Data yang dikirim melebihi batas 1 MiB.")
			return
		}
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			token = r.PostFormValue("csrf_token")
		}
		if auth.CheckCSRF(reqFrom(r.Context()).Session, token) != nil {
			s.errorPage(w, r, http.StatusForbidden, "Permintaan ditolak", "Token keamanan tidak valid atau sudah kedaluwarsa. Muat ulang halaman lalu coba lagi.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host == host && u.User == nil
}

func (s *Server) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	target := "/login"
	if r.Method == http.MethodGet && r.URL.Path != "/" {
		target += "?next=" + url.QueryEscape(r.URL.RequestURI())
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func cookieValue(r *http.Request, name string) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

// safeNext hanya menerima path lokal agar tidak menjadi open redirect.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") {
		return "/dashboard"
	}
	return next
}