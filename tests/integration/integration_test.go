package integration

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/admin"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/config"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/migration"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/platformdb"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/web"
	"github.com/joho/godotenv"
)

type stack struct {
	t         *testing.T
	ctx       context.Context
	db        *sql.DB
	target    *sql.DB
	targetDSN string
	admin     *admin.Service
	ts        *httptest.Server
	suffix    string
}

func newStack(t *testing.T) *stack {
	t.Helper()
	dsn, targetDSN := os.Getenv("TEST_PLATFORM_DATABASE_URL"), os.Getenv("TEST_TARGET_DATABASE_URL")
	if dsn == "" || targetDSN == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL dan TEST_TARGET_DATABASE_URL harus di-set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	db, err := platformdb.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := platformdb.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	target, err := database.OpenTarget(ctx, targetDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { target.Close() })

	cfg, err := config.FromLookup(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := auth.NewManager(auth.PostgresStore{DB: db}, cfg.Session)
	if err != nil {
		t.Fatal(err)
	}
	accessSvc := access.Service{Repo: access.PostgresRepository{DB: db}}
	registry := database.NewRegistry()
	t.Cleanup(func() { registry.CloseAll() })
	adminSvc, err := admin.New(db, registry, sessions, cfg.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	migrationSvc := migration.NewService(db, registry, migration.AccessAuthorizer{Access: accessSvc})
	srv, err := web.New(web.Deps{
		Config: cfg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: sessions, Users: auth.PostgresUsers{DB: db}, Admin: adminSvc,
		Viewer:     web.DatabaseViewer{Access: accessSvc, Conns: adminSvc, Registry: registry},
		Migrations: web.MigrationAdapter{Service: migrationSvc},
		Health:     db.PingContext,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &stack{t: t, ctx: ctx, db: db, target: target, targetDSN: targetDSN, admin: adminSvc, ts: ts, suffix: fmt.Sprint(time.Now().UnixNano() % 1_000_000_000)}
}

func TestMain(m *testing.M) {
	_ = godotenv.Load("../../.env")
	os.Exit(m.Run())
}

func (s *stack) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (s *stack) do(c *http.Client, method, path string, form url.Values) (*http.Response, string) {
	s.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, s.ts.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (s *stack) post(c *http.Client, path string, form url.Values) *http.Response {
	s.t.Helper()
	_, page := s.do(c, "GET", "/dashboard", nil)
	m := csrfRe.FindStringSubmatch(page)
	if m == nil {
		s.t.Fatal("token csrf tidak ditemukan")
	}
	form.Set("csrf_token", m[1])
	resp, _ := s.do(c, "POST", path, form)
	return resp
}

func (s *stack) login(user string) *http.Client {
	s.t.Helper()
	c := s.client()
	resp, _ := s.do(c, "POST", "/login", url.Values{"username": {user}, "password": {"password-1234"}})
	if resp.StatusCode != http.StatusSeeOther {
		s.t.Fatalf("login %s gagal: %d", user, resp.StatusCode)
	}
	return c
}

func (s *stack) user(name string, isAdmin bool) {
	s.t.Helper()
	u, err := s.admin.CreateUser(s.ctx, name, "password-1234", isAdmin)
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { s.db.Exec(`DELETE FROM users WHERE id=$1`, u.ID) })
}

func TestEndToEndAdminToViewer(t *testing.T) {
	s := newStack(t)
	adminName, viewer, editor := "it-admin-"+s.suffix, "it-view-"+s.suffix, "it-edit-"+s.suffix
	s.user(adminName, true)
	s.user(viewer, false)
	s.user(editor, false)

	// Data contoh di database target.
	schema := "it_" + s.suffix
	for _, q := range []string{
		`CREATE SCHEMA ` + schema,
		`CREATE TABLE ` + schema + `.items (id integer primary key, nama text not null, catatan text)`,
		`INSERT INTO ` + schema + `.items SELECT g, 'item ' || g, CASE WHEN g % 2 = 0 THEN NULL ELSE '<b>x</b>' END FROM generate_series(1, 75) g`,
	} {
		if _, err := s.target.ExecContext(s.ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { s.target.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`) })

	// --- Admin mendaftarkan koneksi, group, anggota, dan akses lewat HTTP.
	adm := s.login(adminName)
	tu, _ := url.Parse(s.targetDSN)
	pw, _ := tu.User.Password()
	connName := "it-conn-" + s.suffix
	resp := s.post(adm, "/admin/connections", url.Values{
		"name": {connName}, "host": {tu.Hostname()}, "port": {tu.Port()},
		"database": {strings.TrimPrefix(tu.Path, "/")}, "username": {tu.User.Username()}, "password": {pw}, "ssl_mode": {"disable"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("tambah koneksi: %d", resp.StatusCode)
	}
	var connID int64
	if err := s.db.QueryRowContext(s.ctx, `SELECT id FROM database_connections WHERE name=$1`, connName).Scan(&connID); err != nil {
		t.Fatalf("koneksi tidak tersimpan: %v", err)
	}
	t.Cleanup(func() { s.db.Exec(`DELETE FROM database_connections WHERE id=$1`, connID) })

	groupName := "it-group-" + s.suffix
	s.post(adm, "/admin/groups", url.Values{"name": {groupName}})
	var groupID int64
	if err := s.db.QueryRowContext(s.ctx, `SELECT id FROM app_groups WHERE name=$1`, groupName).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Exec(`DELETE FROM app_groups WHERE id=$1`, groupID) })

	uid := func(name string) string {
		var id int64
		s.db.QueryRowContext(s.ctx, `SELECT id FROM users WHERE username=$1`, name).Scan(&id)
		return strconv.FormatInt(id, 10)
	}
	g := "/admin/groups/" + strconv.FormatInt(groupID, 10)
	s.post(adm, g+"/members", url.Values{"user_id": {uid(viewer)}})
	s.post(adm, g+"/access", url.Values{"database_id": {strconv.FormatInt(connID, 10)}, "level": {"VIEW_ONLY"}})
	if _, body := s.do(adm, "GET", g, nil); !strings.Contains(body, viewer) || !strings.Contains(body, connName) {
		t.Fatal("halaman group harus menampilkan anggota dan akses")
	}
	if _, body := s.do(adm, "GET", "/admin/connections", nil); !strings.Contains(body, connName) || !strings.Contains(body, "Aktif") {
		t.Fatal("koneksi harus tampil dan aktif")
	}

	db := "/databases/" + strconv.FormatInt(connID, 10)

	// --- Pengguna View Only: melihat, tidak bisa menjalankan migration.
	v := s.login(viewer)
	if _, body := s.do(v, "GET", "/dashboard", nil); !strings.Contains(body, connName) || !strings.Contains(body, "View Only") {
		t.Fatal("viewer harus melihat database dengan label View Only")
	}
	if _, body := s.do(v, "GET", db+"?schema="+schema, nil); !strings.Contains(body, "items") {
		t.Fatal("daftar tabel harus memuat items")
	}
	if _, body := s.do(v, "GET", db+"/tables/"+schema+"/items", nil); !strings.Contains(body, "nama") || !strings.Contains(body, "PRIMARY KEY") || !strings.Contains(body, "items_pkey") {
		t.Fatal("struktur tabel harus memuat kolom, constraint, dan index")
	}
	_, body := s.do(v, "GET", db+"/tables/"+schema+"/items/data", nil)
	if !strings.Contains(body, "Baris 1–50 dari 75") || !strings.Contains(body, `class="null">NULL<`) || strings.Contains(body, "<b>x</b>") || !strings.Contains(body, "&lt;b&gt;x&lt;/b&gt;") {
		t.Fatal("data harus berpagination, menandai NULL, dan meng-escape HTML")
	}
	if _, body = s.do(v, "GET", db+"/tables/"+schema+"/items/data?page=2", nil); !strings.Contains(body, "Baris 51–75 dari 75") {
		t.Fatal("halaman 2 tidak sesuai")
	}
	if r, _ := s.do(v, "GET", db+"/tables/"+schema+"/tidak_ada", nil); r.StatusCode != http.StatusNotFound {
		t.Fatalf("tabel tidak ada: %d", r.StatusCode)
	}
	if r := s.post(v, db+"/migrations", url.Values{"sql": {"DROP TABLE " + schema + ".items"}}); r.StatusCode != http.StatusForbidden {
		t.Fatalf("View Only menjalankan migration langsung: %d, ingin 403", r.StatusCode)
	}
	if _, body := s.do(v, "GET", db+"/migrations", nil); !strings.Contains(body, "Akses View Only") || strings.Contains(body, `name="sql"`) {
		t.Fatal("View Only harus dapat melihat riwayat tanpa editor migration")
	}
	var n int
	s.target.QueryRowContext(s.ctx, `SELECT COUNT(*) FROM `+schema+`.items`).Scan(&n)
	if n != 75 {
		t.Fatal("tabel target tidak boleh berubah")
	}

	// --- Pengguna tanpa group: tidak melihat apa pun dan ditolak langsung.
	e := s.login(editor)
	if _, body := s.do(e, "GET", "/dashboard", nil); strings.Contains(body, connName) {
		t.Fatal("pengguna tanpa akses tidak boleh melihat database")
	}
	if r, _ := s.do(e, "GET", db, nil); r.StatusCode != http.StatusForbidden {
		t.Fatalf("akses langsung tanpa izin: %d, ingin 403", r.StatusCode)
	}
	if r, _ := s.do(e, "GET", "/admin/users", nil); r.StatusCode != http.StatusForbidden {
		t.Fatalf("halaman admin oleh non-admin: %d", r.StatusCode)
	}

	// --- Naikkan ke View + Edit lewat group lain: akses efektif tertinggi menang.
	g2 := "it-edit-group-" + s.suffix
	s.post(adm, "/admin/groups", url.Values{"name": {g2}})
	var g2ID int64
	s.db.QueryRowContext(s.ctx, `SELECT id FROM app_groups WHERE name=$1`, g2).Scan(&g2ID)
	t.Cleanup(func() { s.db.Exec(`DELETE FROM app_groups WHERE id=$1`, g2ID) })
	g2p := "/admin/groups/" + strconv.FormatInt(g2ID, 10)
	s.post(adm, g2p+"/members", url.Values{"user_id": {uid(viewer)}})
	s.post(adm, g2p+"/access", url.Values{"database_id": {strconv.FormatInt(connID, 10)}, "level": {"VIEW_EDIT"}})
	if _, body := s.do(v, "GET", "/dashboard", nil); !strings.Contains(body, "View &#43; Edit") {
		t.Fatal("akses efektif harus menjadi View + Edit")
	}
	if r := s.post(v, db+"/migrations", url.Values{"sql": {"SELECT 1"}}); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("View + Edit harus dapat menjalankan migration, dapat %d", r.StatusCode)
	}
	if _, body := s.do(v, "GET", db+"/migrations", nil); !strings.Contains(body, "v1") || !strings.Contains(body, "SELECT 1") {
		t.Fatal("riwayat migration harus menampilkan versi dan SQL yang dijalankan")
	}

	// --- Hapus koneksi: viewer kehilangan akses dan Registry dibersihkan.
	s.post(adm, "/admin/connections/"+strconv.FormatInt(connID, 10)+"/delete", url.Values{})
	if r, _ := s.do(v, "GET", db, nil); r.StatusCode != http.StatusForbidden {
		t.Fatalf("setelah koneksi dihapus: %d, ingin 403", r.StatusCode)
	}

	// --- Logout mencabut session.
	s.post(v, "/logout", url.Values{})
	if r, _ := s.do(v, "GET", "/dashboard", nil); r.StatusCode != http.StatusSeeOther {
		t.Fatal("setelah logout dashboard harus mengarahkan ke login")
	}
}

func TestDeletedUserSessionIsRejected(t *testing.T) {
	s := newStack(t)
	name := "it-del-" + s.suffix
	s.user(name, false)
	c := s.login(name)
	if r, _ := s.do(c, "GET", "/dashboard", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("dashboard: %d", r.StatusCode)
	}
	var id int64
	s.db.QueryRowContext(s.ctx, `SELECT id FROM users WHERE username=$1`, name).Scan(&id)
	if err := s.admin.DeleteUser(s.ctx, id); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.do(c, "GET", "/dashboard", nil); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("session user terhapus harus ditolak, dapat %d", r.StatusCode)
	}
}