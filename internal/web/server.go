package web

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/config"
	webassets "github.com/alyanrrhma/TubesWBD-K02-08/web"
)

type Deps struct {
	Config     *config.Config
	Logger     *slog.Logger
	Sessions   *auth.Manager
	Users      auth.UserRepository
	Admin      AdminService
	Viewer     Viewer
	Migrations Migrations
	Health     func(context.Context) error // boleh nil
}

type Server struct {
	Deps
	pages map[string]*pageTemplate
}

func New(d Deps) (*Server, error) {
	pages, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	return &Server{Deps: d, pages: pages}, nil
}

// Handler mengembalikan seluruh routing aplikasi lengkap dengan middleware global
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	static, _ := fs.Sub(webassets.FS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.staticCache(http.FileServerFS(static))))
	mux.HandleFunc("GET /healthz", s.healthz)

	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginSubmit)

	// Terlindungi: wajib session; method non-GET wajib Origin sesuai + token CSRF
	authed := func(h http.HandlerFunc) http.Handler { return s.requireAuth(s.csrfProtect(h)) }
	admin := func(h http.HandlerFunc) http.Handler {
		return s.requireAuth(s.requireAdmin(s.csrfProtect(h)))
	}

	mux.Handle("GET /{$}", authed(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	}))
	mux.Handle("POST /logout", authed(s.logout))
	mux.Handle("GET /dashboard", authed(s.dashboard))

	// Viewer dan migration. Nama wildcard {databaseID} sama dengan yang dipakai access.Middleware
	mux.Handle("GET /databases/{databaseID}", authed(s.databasePage))
	mux.Handle("GET /databases/{databaseID}/tables/{schema}/{table}", authed(s.tableStructure))
	mux.Handle("GET /databases/{databaseID}/tables/{schema}/{table}/data", authed(s.tableData))
	mux.Handle("GET /databases/{databaseID}/migrations", authed(s.migrationsPage))
	mux.Handle("GET /databases/{databaseID}/migrations/{version}", authed(s.migrationDetailPage))
	mux.Handle("POST /databases/{databaseID}/migrations/explain", authed(s.migrationsExplain))
	mux.Handle("GET /databases/{databaseID}/schema-diff", authed(s.schemaDiffPage))
	mux.Handle("POST /databases/{databaseID}/schema-diff", authed(s.schemaDiffSubmit))
	mux.Handle("POST /databases/{databaseID}/migrations", authed(s.migrationsExecute))

	// Admin (hanya Platform Admin)
	mux.Handle("GET /admin", admin(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}))
	mux.Handle("GET /admin/users", admin(s.usersPage))
	mux.Handle("POST /admin/users", admin(s.usersCreate))
	mux.Handle("POST /admin/users/{id}/delete", admin(s.usersDelete))

	mux.Handle("GET /admin/groups", admin(s.groupsPage))
	mux.Handle("POST /admin/groups", admin(s.groupsCreate))
	mux.Handle("GET /admin/groups/{id}", admin(s.groupPage))
	mux.Handle("POST /admin/groups/{id}/delete", admin(s.groupsDelete))
	mux.Handle("POST /admin/groups/{id}/members", admin(s.membersAdd))
	mux.Handle("POST /admin/groups/{id}/members/{userID}/remove", admin(s.membersRemove))
	mux.Handle("POST /admin/groups/{id}/access", admin(s.accessGrant))
	mux.Handle("POST /admin/groups/{id}/access/{dbID}/revoke", admin(s.accessRevoke))

	mux.Handle("GET /admin/connections", admin(s.connectionsPage))
	mux.Handle("POST /admin/connections", admin(s.connectionsCreate))
	mux.Handle("POST /admin/connections/{id}", admin(s.connectionsUpdate))
	mux.Handle("POST /admin/connections/{id}/test", admin(s.connectionsTest))
	mux.Handle("POST /admin/connections/{id}/delete", admin(s.connectionsDelete))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.errorPage(w, r, http.StatusNotFound, "Halaman tidak ditemukan", "Alamat yang Anda buka tidak ada.")
	})

	return s.recoverer(s.logging(s.securityHeaders(mux)))
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if s.Health != nil {
		if err := s.Health(r.Context()); err != nil {
			s.Logger.Error("healthcheck gagal", "err", err)
			http.Error(w, "platform db unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
