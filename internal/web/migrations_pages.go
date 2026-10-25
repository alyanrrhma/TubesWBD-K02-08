package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/migration"
)

type migrationsPageData struct {
	DB       *DatabaseView
	History  []MigrationRecord
	Page     int
	Pages    int
	Prev     int
	Next     int
	Draft    string // SQL yang dipertahankan saat eksekusi gagal
	Failure  string // pesan kegagalan eksekusi (rollback)
	Plan     string // JSON execution plan hasil EXPLAIN
	Analyzed bool   // true bila plan memuat hasil aktual dari EXPLAIN ANALYZE
}

type migrationDetailPageData struct {
	DB     *DatabaseView
	Record *MigrationRecord
}

const migrationHistoryPageSize = 25

func (s *Server) migrationsPage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "databaseID")
	if !ok {
		return
	}
	s.renderMigrations(w, r, id, http.StatusOK, "", "", "")
}

func (s *Server) renderMigrations(w http.ResponseWriter, r *http.Request, id int64, status int, draft, failure, plan string) {
	s.renderMigrationsMode(w, r, id, status, draft, failure, plan, false)
}

func (s *Server) renderMigrationsMode(w http.ResponseWriter, r *http.Request, id int64, status int, draft, failure, plan string, analyzed bool) {
	ctx, uid := r.Context(), currentUser(r).ID
	db, err := s.Viewer.Database(ctx, uid, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 {
			s.errorPage(w, r, http.StatusBadRequest, "Halaman tidak valid", "Nomor halaman riwayat migration harus positif.")
			return
		}
		page = parsed
	}
	history, total, err := s.Migrations.History(ctx, uid, id, page, migrationHistoryPageSize)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	totalPages := (total + migrationHistoryPageSize - 1) / migrationHistoryPageSize
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		s.errorPage(w, r, http.StatusNotFound, "Halaman tidak ditemukan", "Halaman riwayat migration yang diminta tidak ada.")
		return
	}
	s.render(w, r, status, "migrations", "Migration · "+db.Name, "dashboard",
		migrationsPageData{DB: db, History: history, Page: page, Pages: totalPages, Prev: page - 1, Next: page + 1, Draft: draft, Failure: failure, Plan: plan, Analyzed: analyzed})
}

func (s *Server) migrationsExplain(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "databaseID")
	if !ok {
		return
	}
	sql := r.PostFormValue("sql")
	mode := r.PostFormValue("mode")
	analyze := mode == "analyze"
	if mode != "" && mode != "explain" && !analyze {
		s.errorPage(w, r, http.StatusBadRequest, "Mode analisis tidak valid", "Pilih EXPLAIN atau EXPLAIN ANALYZE.")
		return
	}
	plan, err := s.Migrations.Explain(r.Context(), currentUser(r).ID, id, sql, analyze)
	if err != nil {
		render := func(status int, failure string) {
			s.renderMigrationsMode(w, r, id, status, sql, failure, "", analyze)
		}
		switch {
		case errors.Is(err, migration.ErrExplainSingleStatement):
			render(http.StatusUnprocessableEntity, "Analisis hanya menerima satu perintah SQL tanpa titik koma.")
		case errors.Is(err, migration.ErrEmptySQL):
			render(http.StatusUnprocessableEntity, "SQL migration tidak boleh kosong.")
		case errors.Is(err, migration.ErrInvalidSQL):
			render(http.StatusBadRequest, "SQL migration berisi karakter yang tidak valid.")
		case errors.Is(err, migration.ErrSQLTooLarge):
			s.errorPage(w, r, http.StatusRequestEntityTooLarge, "SQL terlalu besar", "Ukuran SQL migration melewati batas 1 MiB.")
		case errors.Is(err, access.ErrForbidden):
			s.fail(w, r, err)
		default:
			failure := strings.TrimPrefix(err.Error(), "explain migration SQL: ")
			failure = strings.TrimPrefix(failure, "explain analyze migration SQL: ")
			render(http.StatusUnprocessableEntity, "Execution plan tidak dapat dibuat: "+failure)
		}
		return
	}
	s.renderMigrationsMode(w, r, id, http.StatusOK, sql, "", plan, analyze)
}

func (s *Server) migrationsExecute(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "databaseID")
	if !ok {
		return
	}
	sql := r.PostFormValue("sql")
	rec, err := s.Migrations.Execute(r.Context(), currentUser(r).ID, id, sql)
	if err != nil {
		switch {
		case errors.Is(err, migration.ErrEmptySQL):
			s.renderMigrations(w, r, id, http.StatusUnprocessableEntity, sql, "SQL migration tidak boleh kosong.", "")
		case errors.Is(err, migration.ErrInvalidSQL):
			s.renderMigrations(w, r, id, http.StatusBadRequest, sql, "SQL migration berisi karakter yang tidak valid.", "")
		case errors.Is(err, migration.ErrSQLTooLarge):
			s.errorPage(w, r, http.StatusRequestEntityTooLarge, "SQL terlalu besar", "Ukuran SQL migration melewati batas 1 MiB.")
		default:
			s.fail(w, r, err)
		}
		return
	}
	if rec.Status == MigrationFailed {
		msg := "Migration gagal dan perubahan di-rollback."
		if rec.ErrorMessage != "" {
			msg += " " + rec.ErrorMessage
		}
		s.renderMigrations(w, r, id, http.StatusUnprocessableEntity, sql, msg, "")
		return
	}
	s.setFlash(w, "success", "Migration berhasil di-commit.")
	http.Redirect(w, r, "/databases/"+r.PathValue("databaseID")+"/migrations", http.StatusSeeOther)
}

func (s *Server) migrationDetailPage(w http.ResponseWriter, r *http.Request) {
	databaseID, ok := s.pathID(w, r, "databaseID")
	if !ok {
		return
	}

	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || version < 1 {
		s.errorPage(
			w,
			r,
			http.StatusBadRequest,
			"Versi tidak valid",
			"Versi migration harus berupa bilangan positif.",
		)
		return
	}

	ctx := r.Context()
	userID := currentUser(r).ID

	db, err := s.Viewer.Database(ctx, userID, databaseID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	record, err := s.Migrations.Detail(ctx, userID, databaseID, version)
	if errors.Is(err, migration.ErrMigrationNotFound) {
		s.errorPage(
			w,
			r,
			http.StatusNotFound,
			"Migration tidak ditemukan",
			"Versi migration yang diminta tidak ada pada database ini.",
		)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}

	s.render(
		w,
		r,
		http.StatusOK,
		"migration_detail",
		"Detail Migration · "+db.Name,
		"dashboard",
		migrationDetailPageData{
			DB:     db,
			Record: record,
		},
	)
}
