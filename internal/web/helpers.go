package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/admin"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/migration"
)

type errorData struct {
	Status  int
	Message string
}

func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	s.render(w, r, status, "error", title, "", errorData{Status: status, Message: msg})
}

// fail memetakan error service ke respons yang aman: pesan mentah tidak ditampilkan (bisa memuat detail internal), hanya dicatat di log
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		s.endSession(w)
		s.redirectToLogin(w, r)
	case errors.Is(err, access.ErrForbidden):
		s.errorPage(w, r, http.StatusForbidden, "Akses ditolak", "Anda tidak memiliki izin untuk melakukan tindakan ini.")
	case errors.Is(err, admin.ErrNotFound), errors.Is(err, database.ErrTableNotFound):
		s.errorPage(w, r, http.StatusNotFound, "Data tidak ditemukan", "Data yang Anda cari tidak ada atau sudah dihapus.")
	case errors.Is(err, database.ErrNotRegistered):
		s.errorPage(w, r, http.StatusServiceUnavailable, "Koneksi belum aktif", "Koneksi ke database target belum aktif. Minta Platform Admin menjalankan tes koneksi di halaman Koneksi.")
	case errors.Is(err, database.ErrConnectFailed):
		s.errorPage(w, r, http.StatusBadGateway, "Koneksi database gagal", "Tidak dapat terhubung ke database target. Periksa pengaturan koneksi atau hubungi administrator.")
	case errors.Is(err, database.ErrTargetPermissionDenied):
		s.errorPage(w, r, http.StatusBadGateway, "Database target menolak akses", "Akun koneksi tidak memiliki hak yang cukup di database target. Hubungi administrator.")
	case errors.Is(err, database.ErrInvalidArgument):
		s.errorPage(w, r, http.StatusBadRequest, "Permintaan tidak valid", "Parameter yang dikirim tidak valid.")
	case errors.Is(err, migration.ErrMigrationNotFound):
		s.errorPage(w, r, http.StatusNotFound, "Migration tidak ditemukan", "Riwayat migration yang Anda cari tidak ada.")
	case errors.Is(err, migration.ErrSQLTooLarge):
		s.errorPage(w, r, http.StatusRequestEntityTooLarge, "SQL terlalu besar", "Ukuran SQL migration melewati batas 1 MiB.")
	case errors.Is(err, migration.ErrEmptySQL), errors.Is(err, migration.ErrInvalidSQL), errors.Is(err, migration.ErrInvalidArgument):
		s.errorPage(w, r, http.StatusBadRequest, "SQL tidak valid", "Periksa kembali SQL migration yang dikirim.")
	default:
		s.Logger.Error("error tak terduga", "path", r.URL.Path, "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "Terjadi kesalahan", "Permintaan tidak dapat diproses. Coba lagi beberapa saat lagi.")
	}
}

// done dipakai handler POST admin: error input -> flash di halaman asal; error lain -> fail.
func (s *Server) done(w http.ResponseWriter, r *http.Request, redirect, okMsg string, err error) {
	if err != nil {
		var ie *admin.InputError
		if errors.As(err, &ie) {
			s.setFlash(w, "error", ie.Message)
			http.Redirect(w, r, redirect, http.StatusSeeOther)
			return
		}
		s.fail(w, r, err)
		return
	}
	s.setFlash(w, "success", okMsg)
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (s *Server) pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id < 1 {
		s.errorPage(w, r, http.StatusNotFound, "Halaman tidak ditemukan", "Alamat yang Anda buka tidak valid.")
		return 0, false
	}
	return id, true
}

func formInt64(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PostFormValue(name), 10, 64)
	return id, err == nil && id > 0
}

func badInput(msg string) error { return &admin.InputError{Message: msg} }

func isConnFailed(err error) bool { return errors.Is(err, database.ErrConnectFailed) }