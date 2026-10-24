package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	webassets "github.com/alyanrrhma/TubesWBD-K02-08/web"
)

type pageTemplate = template.Template

type flash struct{ Kind, Message string }

// pageData adalah data yang diterima layout.html; halaman membaca data spesifiknya lewat .Data.
type pageData struct {
	Title string
	Nav   string // penanda menu aktif
	User  *userInfo
	CSRF  string
	Flash *flash
	Data  any
}

type userInfo struct {
	ID       int64
	Username string
	IsAdmin  bool
}

var templateFuncs = template.FuncMap{
	"fmtTime": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.UTC().Format("2 Jan 2006, 15:04") + " UTC"
	},
	// pathseg meng-escape satu segmen path URL (nama tabel bisa berisi karakter khusus).
	"pathseg": url.PathEscape,
	"dbAddr":  func(host string, port int, db string) string { return fmt.Sprintf("%s:%d/%s", host, port, db) },
	"levelLabel": func(l access.Level) string {
		switch l {
		case access.ViewOnly:
			return "View Only"
		case access.ViewEdit:
			return "View + Edit"
		}
		return "Tanpa akses"
	},
	"levelClass": func(l access.Level) string {
		if l == access.ViewEdit {
			return "edit"
		}
		return "view"
	},
	"canEdit": func(l access.Level) bool { return l == access.ViewEdit },
}

// loadTemplates memuat layout.html + satu file di pages/ per halaman, masing-masing dengan set template sendiri sehingga {{define "content"}} tidak bentrok.
func loadTemplates() (map[string]*pageTemplate, error) {
	files, err := fs.Glob(webassets.FS, "templates/pages/*.html")
	if err != nil {
		return nil, err
	}
	pages := make(map[string]*pageTemplate, len(files))
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".html")
		t, err := template.New("layout.html").Funcs(templateFuncs).ParseFS(webassets.FS, "templates/layout.html", f)
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", name, err)
		}
		pages[name] = t
	}
	return pages, nil
}

// render menulis halaman penuh. Output di-buffer agar error template tidak meninggalkan respons setengah jadi.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page, title, nav string, data any) {
	t, ok := s.pages[page]
	if !ok {
		s.Logger.Error("template tidak ditemukan", "page", page)
		http.Error(w, "Terjadi kesalahan pada server.", http.StatusInternalServerError)
		return
	}
	pd := pageData{Title: title, Nav: nav, Data: data, Flash: s.takeFlash(w, r)}
	if rc := reqFrom(r.Context()); rc != nil {
		pd.User, pd.CSRF = &rc.User, rc.CSRF
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", pd); err != nil {
		s.Logger.Error("render gagal", "page", page, "err", err)
		http.Error(w, "Terjadi kesalahan pada server.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store") // halaman memuat data database; jangan di-cache
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// flash message

const flashCookie = "dbmp_flash"

func (s *Server) cookieBase(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.Config.Session.SecureCookies, SameSite: http.SameSiteLaxMode,
	}
}

func (s *Server) setFlash(w http.ResponseWriter, kind, msg string) {
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300])
	}
	http.SetCookie(w, s.cookieBase(flashCookie, url.QueryEscape(kind+"|"+msg), 60))
}

func (s *Server) takeFlash(w http.ResponseWriter, r *http.Request) *flash {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return nil
	}
	http.SetCookie(w, s.cookieBase(flashCookie, "", -1))
	raw, err := url.QueryUnescape(c.Value)
	if err != nil {
		return nil
	}
	kind, msg, ok := strings.Cut(raw, "|")
	if !ok || msg == "" {
		return nil
	}
	if kind != "success" && kind != "error" {
		kind = "info"
	}
	return &flash{Kind: kind, Message: msg}
}