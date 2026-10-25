package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/admin"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/config"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/web"
)

var (
	hashOnce sync.Once
	testHash string
)

func passwordHash(t *testing.T) string {
	t.Helper()
	hashOnce.Do(func() {
		h, err := auth.HashPassword("password-1234")
		if err != nil {
			panic(err)
		}
		testHash = h
	})
	return testHash
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type env struct {
	t              *testing.T
	ts             *httptest.Server
	users          *fakeUsers
	access         fakeAccess
	viewer         *fakeViewer
	migs           *recordingMigrations
	clock          *clock
	mgr            *auth.Manager
	admin          *fakeAdmin
	salesID, invID int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	cfg, err := config.FromLookup(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{t: time.Now()}
	mgr, err := auth.NewManager(auth.NewMemoryStore(), cfg.Session)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Now = clk.Now

	users := newFakeUsers(passwordHash(t))
	e := &env{t: t, users: users, clock: clk, mgr: mgr, salesID: 100, invID: 101}
	e.access = fakeAccess{levels: map[[2]int64]access.Level{
		{users.id("alice"), e.salesID}: access.ViewEdit,
		{users.id("bob"), e.salesID}:   access.ViewOnly,
		{users.id("bob"), e.invID}:     access.ViewOnly,
	}}
	e.viewer = &fakeViewer{
		access: e.access,
		dbs: map[int64]web.DatabaseView{
			e.salesID: {ID: e.salesID, Name: "Sales DB", Host: "localhost", Port: 5433, Database: "sales", Active: true},
			e.invID:   {ID: e.invID, Name: "Inventory DB", Host: "localhost", Port: 5433, Database: "inventory", Active: true},
		},
		rowsFor: func(page int) *web.TableDataView {
			d := &web.TableDataView{Columns: []string{"id", "email"}, Page: page, PageSize: 50, Total: 120}
			if page > 3 {
				return d
			}
			start := (page - 1) * 50
			for i := start; i < start+50 && i < 120; i++ {
				cell := web.CellView{Text: "u@example.com"}
				if i%2 == 1 {
					cell = web.CellView{Null: true}
				}
				d.Rows = append(d.Rows, []web.CellView{{Text: "n"}, cell})
			}
			return d
		},
	}
	e.migs = &recordingMigrations{access: e.access}
	adminSvc := &fakeAdmin{users: users, connections: []admin.Connection{{ID: 1, Name: "Target", Host: "localhost", Port: 5433, Database: "dbmp_target", Username: "dbmp", SSLMode: "disable", Complete: true}}}
	e.admin = adminSvc

	srv, err := web.New(web.Deps{
		Config: cfg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: mgr, Users: users, Admin: adminSvc,
		Viewer: e.viewer, Migrations: e.migs,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.ts = httptest.NewServer(srv.Handler())
	t.Cleanup(e.ts.Close)
	return e
}

// client baru dengan cookie jar; redirect tidak diikuti agar bisa diperiksa
func (e *env) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (e *env) do(c *http.Client, req *http.Request) (*http.Response, string) {
	e.t.Helper()
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (e *env) get(c *http.Client, path string) (*http.Response, string) {
	req, _ := http.NewRequest("GET", e.ts.URL+path, nil)
	return e.do(c, req)
}

func (e *env) post(c *http.Client, path string, form url.Values) (*http.Response, string) {
	req, _ := http.NewRequest("POST", e.ts.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return e.do(c, req)
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// postCSRF mengambil token CSRF dari halaman /dashboard lalu mengirim form
func (e *env) postCSRF(c *http.Client, path string, form url.Values) (*http.Response, string) {
	e.t.Helper()
	_, page := e.get(c, "/dashboard")
	m := csrfRe.FindStringSubmatch(page)
	if m == nil {
		e.t.Fatal("token csrf tidak ditemukan di halaman")
	}
	form.Set("csrf_token", m[1])
	return e.post(c, path, form)
}

// login membuat session langsung lewat auth.Manager (tanpa PBKDF2) agar test cepat;
// alur login lewat HTTP diuji khusus di TestLoginFailureAndSuccess
func (e *env) login(user string) *http.Client {
	e.t.Helper()
	c := e.client()
	rec := httptest.NewRecorder()
	u := e.users.users[user]
	csrf, err := e.mgr.Login(context.Background(), rec, auth.Principal{UserID: u.ID, Admin: u.IsAdmin})
	if err != nil {
		e.t.Fatal(err)
	}
	cookies := append(rec.Result().Cookies(), &http.Cookie{Name: "dbmp_csrf", Value: csrf, Path: "/"})
	base, _ := url.Parse(e.ts.URL)
	c.Jar.SetCookies(base, cookies)
	return c
}

func cookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func sales(e *env) string { return "/databases/100" }

func TestUnauthenticatedRedirectsToLogin(t *testing.T) {
	e := newEnv(t)
	resp, _ := e.get(e.client(), "/databases/100")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login?next=") {
		t.Fatalf("status=%d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestLoginFailureAndSuccess(t *testing.T) {
	e := newEnv(t)
	c := e.client()

	for _, form := range []url.Values{
		{"username": {"alice"}, "password": {"salah-salah"}},
		{"username": {"tidak-ada"}, "password": {"password-1234"}},
		{"username": {""}, "password": {""}},
	} {
		resp, body := e.post(c, "/login", form)
		if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "Username atau password salah.") {
			t.Fatalf("%v: login gagal harus 401 dengan pesan generik, dapat %d", form, resp.StatusCode)
		}
		if cookie(resp, auth.CookieName) != nil {
			t.Fatal("login gagal tidak boleh membuat cookie session")
		}
	}

	resp, _ := e.post(c, "/login", url.Values{"username": {"alice"}, "password": {"password-1234"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/dashboard" {
		t.Fatalf("status=%d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
	for _, name := range []string{auth.CookieName, "dbmp_csrf"} {
		ck := cookie(resp, name)
		if ck == nil || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" {
			t.Fatalf("atribut cookie %s tidak sesuai: %+v", name, ck)
		}
	}
}

func TestLoginRejectsForeignOrigin(t *testing.T) {
	e := newEnv(t)
	req, _ := http.NewRequest("POST", e.ts.URL+"/login", strings.NewReader("username=alice&password=password-1234"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	resp, _ := e.do(e.client(), req)
	if resp.StatusCode != http.StatusForbidden || cookie(resp, auth.CookieName) != nil {
		t.Fatalf("Origin asing harus ditolak, dapat %d", resp.StatusCode)
	}
}

func TestLoginNextIsNotOpenRedirect(t *testing.T) {
	e := newEnv(t)
	for next, want := range map[string]string{
		"//evil.example":       "/dashboard",
		"https://evil.example": "/dashboard",
		"/\\evil":              "/dashboard",
		"/databases/100":       "/databases/100",
	} {
		resp, _ := e.post(e.client(), "/login", url.Values{"username": {"alice"}, "password": {"password-1234"}, "next": {next}})
		if got := resp.Header.Get("Location"); got != want {
			t.Errorf("next=%q -> %q, ingin %q", next, got, want)
		}
	}
}

func TestSecondLoginRevokesPreviousSession(t *testing.T) {
	e := newEnv(t)
	c := e.login("alice")
	u, _ := url.Parse(e.ts.URL)
	var old string
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == auth.CookieName {
			old = ck.Value
		}
	}
	if old == "" {
		t.Fatal("cookie session tidak ada")
	}
	// Login ulang dengan klien yang sama (cookie lama terkirim) -> session lama harus dicabut.
	e.post(c, "/login", url.Values{"username": {"alice"}, "password": {"password-1234"}})
	other := e.client()
	other.Jar.SetCookies(u, []*http.Cookie{{Name: auth.CookieName, Value: old, Path: "/"}})
	if resp, _ := e.get(other, "/dashboard"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("session lama harus tidak berlaku, dapat %d", resp.StatusCode)
	}
}

func TestDashboardShowsOnlyAccessibleDatabases(t *testing.T) {
	e := newEnv(t)
	_, body := e.get(e.login("alice"), "/dashboard")
	if !strings.Contains(body, "Sales DB") || strings.Contains(body, "Inventory DB") || !strings.Contains(body, "View &#43; Edit") {
		t.Fatal("alice hanya boleh melihat Sales DB (View + Edit)")
	}
	_, body = e.get(e.login("bob"), "/dashboard")
	if !strings.Contains(body, "Sales DB") || !strings.Contains(body, "Inventory DB") || !strings.Contains(body, "View Only") {
		t.Fatal("bob harus melihat dua database dengan label View Only")
	}
	_, body = e.get(e.login("admin"), "/dashboard")
	if !strings.Contains(body, "Belum ada database yang bisa Anda akses") {
		t.Fatal("admin tanpa group harus melihat empty state")
	}
}

func TestDirectAccessToForbiddenDatabase(t *testing.T) {
	e := newEnv(t)
	c := e.login("alice") // tidak punya akses ke Inventory DB (101)
	for _, p := range []string{"/databases/101", "/databases/101/migrations", "/databases/101/tables/public/customers", "/databases/101/tables/public/customers/data"} {
		if resp, _ := e.get(c, p); resp.StatusCode != http.StatusForbidden {
			t.Errorf("GET %s: status %d, ingin 403", p, resp.StatusCode)
		}
	}
	resp, _ := e.postCSRF(c, "/databases/101/migrations", url.Values{"sql": {"DROP TABLE x"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST migration tanpa akses: status %d, ingin 403", resp.StatusCode)
	}
	if len(e.migs.history) != 0 {
		t.Fatal("tidak boleh ada migration yang tercatat")
	}
}

func TestCSRFTokenInPageMatchesSessionAndIsRequired(t *testing.T) {
	e := newEnv(t)
	c := e.login("alice")

	_, page := e.get(c, "/dashboard")
	if !csrfRe.MatchString(page) {
		t.Fatal("form logout harus memuat token CSRF")
	}
	if resp, _ := e.post(c, "/logout", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("logout tanpa token: status %d, ingin 403", resp.StatusCode)
	}
	if resp, _ := e.post(c, "/logout", url.Values{"csrf_token": {strings.Repeat("a", 43)}}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("logout dengan token salah: status %d, ingin 403", resp.StatusCode)
	}
	// Origin asing ditolak meski token benar.
	m := csrfRe.FindStringSubmatch(page)
	req, _ := http.NewRequest("POST", e.ts.URL+"/logout", strings.NewReader("csrf_token="+m[1]))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	if resp, _ := e.do(c, req); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Origin asing: status %d, ingin 403", resp.StatusCode)
	}
	// Setelah semua penolakan, session tetap hidup.
	if resp, _ := e.get(c, "/dashboard"); resp.StatusCode != http.StatusOK {
		t.Fatal("session seharusnya masih aktif")
	}
	// Token valid lewat header juga diterima.
	req, _ = http.NewRequest("POST", e.ts.URL+"/logout", nil)
	req.Header.Set("X-CSRF-Token", m[1])
	if resp, _ := e.do(c, req); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout dengan header X-CSRF-Token: status %d", resp.StatusCode)
	}
	if resp, _ := e.get(c, "/dashboard"); resp.StatusCode != http.StatusSeeOther {
		t.Fatal("setelah logout, dashboard harus mengarahkan ke login")
	}
}

func TestMissingCSRFCookieIsReissuedOnGet(t *testing.T) {
	e := newEnv(t)
	c := e.login("alice")
	u, _ := url.Parse(e.ts.URL)
	c.Jar.SetCookies(u, []*http.Cookie{{Name: "dbmp_csrf", Value: "", MaxAge: -1, Path: "/"}})
	resp, page := e.get(c, "/dashboard")
	if cookie(resp, "dbmp_csrf") == nil || !csrfRe.MatchString(page) {
		t.Fatal("cookie CSRF yang hilang harus diterbitkan ulang pada GET")
	}
	if r, _ := e.postCSRF(c, "/logout", url.Values{}); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("token baru harus berlaku, dapat %d", r.StatusCode)
	}
}

func TestSessionExpiresAfterIdleTimeout(t *testing.T) {
	e := newEnv(t)
	c := e.login("alice")
	e.clock.Advance(31 * time.Minute) // idle default 30m
	resp, _ := e.get(c, "/dashboard")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login") {
		t.Fatalf("session idle harus diarahkan ke login, dapat %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, body := e.get(c, "/login")
	if !strings.Contains(body, "Sesi Anda telah berakhir") {
		t.Fatal("pengguna harus diberi tahu bahwa sesi berakhir")
	}
}

func TestSessionRotationKeepsUserLoggedIn(t *testing.T) {
	e := newEnv(t)
	c := e.login("alice")
	e.clock.Advance(16 * time.Minute) // rotation interval 15m, idle 30m
	resp, _ := e.get(c, "/dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ck := cookie(resp, auth.CookieName); ck == nil || ck.Value == "" {
		t.Fatal("cookie session harus dirotasi")
	}
	// Cookie baru dipakai klien; CSRF cookie tetap valid untuk aksi POST.
	if r, _ := e.postCSRF(c, "/logout", url.Values{}); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST setelah rotation: status %d", r.StatusCode)
	}
}

func TestAdminPagesRestrictedToAdmin(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice")
	for _, p := range []string{"/admin", "/admin/users", "/admin/groups", "/admin/connections"} {
		if resp, _ := e.get(alice, p); resp.StatusCode != http.StatusForbidden {
			t.Errorf("non-admin GET %s: status %d, ingin 403", p, resp.StatusCode)
		}
	}
	resp, _ := e.postCSRF(alice, "/admin/users", url.Values{"username": {"x-user"}, "password": {"secret-1234"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST admin oleh non-admin: status %d, ingin 403", resp.StatusCode)
	}
	if resp, _ := e.get(e.login("admin"), "/admin/users"); resp.StatusCode != http.StatusOK {
		t.Fatalf("admin GET /admin/users: status %d", resp.StatusCode)
	}
}

func TestAdminCanEditConnectionAndReenterPassword(t *testing.T) {
	e := newEnv(t)
	adminClient := e.login("admin")
	resp, body := e.get(adminClient, "/admin/connections")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `action="/admin/connections/1"`) || !strings.Contains(body, "Kosongkan untuk mempertahankan password") {
		t.Fatalf("form edit koneksi harus tampil tanpa password tersimpan: status %d", resp.StatusCode)
	}
	form := url.Values{"name": {"Target diperbarui"}, "host": {"localhost"}, "port": {"5433"}, "database": {"dbmp_target"}, "username": {"dbmp"}, "password": {"dbmp_dev_password"}, "ssl_mode": {"disable"}}
	resp, _ = e.postCSRF(adminClient, "/admin/connections/1", form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update koneksi status %d", resp.StatusCode)
	}
	resp, body = e.get(adminClient, "/admin/connections")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Target diperbarui") || !strings.Contains(body, "Koneksi berhasil diperbarui") {
		t.Fatal("perubahan koneksi dan status sukses harus terlihat")
	}
}

func TestAdminCreateUserFlashAndSelfDelete(t *testing.T) {
	e := newEnv(t)
	c := e.login("admin")

	e.postCSRF(c, "/admin/users", url.Values{"username": {"carol"}, "password": {"secret-1234"}})
	_, body := e.get(c, "/admin/users")
	if !strings.Contains(body, "carol") || !strings.Contains(body, "Pengguna berhasil dibuat.") {
		t.Fatal("pengguna baru dan flash sukses harus tampil")
	}
	e.postCSRF(c, "/admin/users", url.Values{"username": {"carol"}, "password": {"secret-1234"}})
	_, body = e.get(c, "/admin/users")
	if !strings.Contains(body, "Username sudah dipakai.") {
		t.Fatal("pesan validasi dari service harus tampil")
	}
	if _, body = e.get(c, "/admin/users"); strings.Contains(body, "Username sudah dipakai.") {
		t.Fatal("flash tidak boleh muncul dua kali")
	}
	e.postCSRF(c, "/admin/users/"+"1"+"/delete", url.Values{}) // id 1 = admin sendiri
	if _, body = e.get(c, "/admin/users"); !strings.Contains(body, "tidak dapat menghapus akun yang sedang dipakai") {
		t.Fatal("menghapus akun sendiri harus ditolak dengan pesan")
	}
}

func TestAdminDeleteUserWithMigrationHistoryShowsFlash(t *testing.T) {
	e := newEnv(t)
	c := e.login("admin")
	e.admin.deleteError = admin.ErrUserHasHistory

	resp, _ := e.postCSRF(c, "/admin/users/3/delete", url.Values{})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/users" {
		t.Fatalf("hapus user ber-riwayat harus kembali ke daftar user, dapat status %d dan Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	resp, body := e.get(c, "/admin/users")
	if resp.StatusCode != http.StatusOK ||
		!strings.Contains(body, "Pengguna tidak dapat dihapus karena pernah menjalankan migration") ||
		!strings.Contains(body, "Untuk mencabut akses") {
		t.Fatal("pesan penolakan harus tampil di halaman daftar pengguna")
	}
}

func TestOutputIsEscaped(t *testing.T) {
	e := newEnv(t)
	c := e.login("admin")
	e.postCSRF(c, "/admin/groups", url.Values{"name": {`<script>alert(1)</script>`}})
	_, body := e.get(c, "/admin/groups")
	if strings.Contains(body, "<script>alert(1)") || !strings.Contains(body, "&lt;script&gt;alert(1)") {
		t.Fatal("nama group harus di-escape")
	}
}

func TestMigrationViewOnlyCannotExecute(t *testing.T) {
	e := newEnv(t)
	bob := e.login("bob")
	_, body := e.get(bob, "/databases/100/migrations")
	if strings.Contains(body, `name="sql"`) || !strings.Contains(body, "Akses View Only") {
		t.Fatal("View Only tidak boleh melihat editor migration")
	}
	resp, _ := e.postCSRF(bob, "/databases/100/migrations", url.Values{"sql": {"DROP TABLE customers;"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("View Only POST migration langsung: status %d, ingin 403", resp.StatusCode)
	}
	resp, _ = e.postCSRF(bob, "/databases/100/migrations/explain", url.Values{"sql": {"SELECT * FROM customers"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("View Only EXPLAIN langsung: status %d, ingin 403", resp.StatusCode)
	}
	if len(e.migs.history) != 0 {
		t.Fatal("history tidak boleh bertambah")
	}
}

func TestMigrationExplainShowsPlanBeforeExecution(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice")
	path := "/databases/100/migrations/explain"
	sql := "SELECT * FROM customers WHERE id = 1"
	resp, body := e.postCSRF(alice, path, url.Values{"sql": {sql}})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Estimated execution plan") || !strings.Contains(body, "Node Type") || !strings.Contains(body, sql) {
		t.Fatalf("EXPLAIN harus menampilkan SQL dan plan: status %d", resp.StatusCode)
	}
	if len(e.migs.history) != 0 {
		t.Fatal("EXPLAIN tidak boleh mengeksekusi atau mencatat migration")
	}
}

func TestSchemaDiffGeneratesEditableCandidateAndRequiresEditAccess(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice")
	_, body := e.get(alice, "/databases/100/schema-diff")
	if !strings.Contains(body, "Schema diff") || !strings.Contains(body, `name="desired_schema"`) || !strings.Contains(body, `"schema":"public"`) {
		t.Fatal("schema diff page should show current schema as editable JSON")
	}
	desired := `{"schema":"public","tables":[]}`
	resp, body := e.postCSRF(alice, "/databases/100/schema-diff", url.Values{"desired_schema": {desired}})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Perubahan schema") || !strings.Contains(body, `name="sql"`) || !strings.Contains(body, "DROP TABLE") || !strings.Contains(body, "Perubahan destruktif") {
		t.Fatalf("schema diff should render the change and editable SQL candidate, status %d", resp.StatusCode)
	}
	if len(e.migs.history) != 0 {
		t.Fatal("schema diff must not execute a migration")
	}
	bob := e.login("bob")
	resp, _ = e.postCSRF(bob, "/databases/100/schema-diff", url.Values{"desired_schema": {desired}})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("View Only schema diff POST: status %d, want 403", resp.StatusCode)
	}
}

func TestMigrationExecuteSuccessFailureAndEmpty(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice")
	path := "/databases/100/migrations"

	resp, _ := e.postCSRF(alice, path, url.Values{"sql": {"ALTER TABLE orders ADD COLUMN note TEXT;"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("migration sukses: status %d", resp.StatusCode)
	}
	_, body := e.get(alice, path)
	if !strings.Contains(body, "Migration berhasil di-commit.") ||
		!strings.Contains(body, `href="/databases/100/migrations/1"`) ||
		!strings.Contains(body, `name="sql"`) {
		t.Fatal("history harus menampilkan tautan v1, flash sukses, dan editor")
	}

	bad := "SELECT fail;"
	resp, body = e.postCSRF(alice, path, url.Values{"sql": {bad}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "di-rollback") || !strings.Contains(body, bad) {
		t.Fatalf("migration gagal: status %d; SQL harus dipertahankan dan rollback dilaporkan", resp.StatusCode)
	}
	resp, body = e.postCSRF(alice, path, url.Values{"sql": {"  \n "}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "tidak boleh kosong") {
		t.Fatalf("SQL kosong: status %d", resp.StatusCode)
	}
}

func TestMigrationHistoryPagination(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice")
	path := "/databases/100/migrations"
	for i := 0; i < 27; i++ {
		resp, _ := e.postCSRF(alice, path, url.Values{"sql": {"SELECT 1"}})
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("migration %d: status %d", i+1, resp.StatusCode)
		}
	}
	_, body := e.get(alice, path+"?page=2")
	if !strings.Contains(body, "Halaman 2 dari 2") ||
		!strings.Contains(body, `href="/databases/100/migrations/2"`) ||
		!strings.Contains(body, `href="/databases/100/migrations/1"`) {
		t.Fatal("halaman kedua harus menampilkan tautan migration tertua dengan navigasi halaman")
	}
	resp, _ := e.get(alice, path+"?page=0")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("page 0 harus ditolak, status %d", resp.StatusCode)
	}
}

func TestTableDataPaginationAndNull(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice")
	base := "/databases/100/tables/public/customers/data"

	_, body := e.get(alice, base)
	if !strings.Contains(body, "Baris 1–50 dari 120") || !strings.Contains(body, "Halaman 1 dari 3") || !strings.Contains(body, `class="null">NULL<`) {
		t.Fatal("halaman 1 harus menampilkan baris 1–50 dari 120 dan menandai NULL")
	}
	_, body = e.get(alice, base+"?page=3")
	if !strings.Contains(body, "Baris 101–120 dari 120") {
		t.Fatal("halaman 3 harus menampilkan baris 101–120")
	}
	resp, _ := e.get(alice, base+"?page=99")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != base+"?page=3" {
		t.Fatalf("di luar jangkauan harus ke halaman terakhir, dapat %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = e.get(alice, base+"?page=999999999")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != base+"?page=3" {
		t.Fatalf("nomor halaman sangat besar harus diarahkan ke halaman terakhir, dapat %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := e.get(alice, "/databases/100/tables/public/customers"); resp.StatusCode != http.StatusOK {
		t.Fatalf("halaman struktur: %d", resp.StatusCode)
	}
	if resp, _ := e.get(alice, "/databases/100?schema=audit"); resp.StatusCode != http.StatusOK {
		t.Fatalf("halaman database: %d", resp.StatusCode)
	}
}

func TestTargetErrorsDoNotLeakDetails(t *testing.T) {
	e := newEnv(t)
	alice := e.login("alice")
	cases := map[error]struct {
		status int
		text   string
	}{
		wrap(database.ErrConnectFailed, "password=s3cr3t"):        {http.StatusBadGateway, "Tidak dapat terhubung ke database target"},
		wrap(database.ErrNotRegistered, "id 100"):                 {http.StatusServiceUnavailable, "belum aktif"},
		wrap(database.ErrTargetPermissionDenied, "host=10.0.0.5"): {http.StatusBadGateway, "menolak akses"},
		wrap(database.ErrTableNotFound, "\"x\".\"y\""):            {http.StatusNotFound, "tidak ditemukan"},
		wrap(database.ErrQueryTimeout, "count table rows"):        {http.StatusGatewayTimeout, "Database target terlalu lama merespons"},
		wrap(database.ErrWriteRejected, "read table data"):        {http.StatusForbidden, "Penulisan ditolak"},
		io.ErrUnexpectedEOF: {http.StatusInternalServerError, "tidak dapat diproses"},
	}
	for err, want := range cases {
		e.viewer.err = err
		resp, body := e.get(alice, "/databases/100")
		if resp.StatusCode != want.status || !strings.Contains(body, want.text) {
			t.Errorf("%v: status %d, ingin %d dengan %q", err, resp.StatusCode, want.status, want.text)
		}
		for _, secret := range []string{"s3cr3t", "10.0.0.5", "unexpected EOF"} {
			if strings.Contains(body, secret) {
				t.Errorf("%v: respons membocorkan %q", err, secret)
			}
		}
	}
}

type wrapped struct {
	sentinel error
	detail   string
}

func wrap(s error, d string) error { return wrapped{s, d} }
func (w wrapped) Error() string    { return w.sentinel.Error() + ": " + w.detail }
func (w wrapped) Unwrap() error    { return w.sentinel }

func TestSecurityHeadersAndHealth(t *testing.T) {
	e := newEnv(t)
	resp, _ := e.get(e.client(), "/login")
	for h, want := range map[string]string{"X-Frame-Options": "DENY", "X-Content-Type-Options": "nosniff"} {
		if resp.Header.Get(h) != want {
			t.Errorf("%s = %q", h, resp.Header.Get(h))
		}
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("CSP tidak ada")
	}
	if resp, body := e.get(e.client(), "/healthz"); resp.StatusCode != 200 || body != "ok" {
		t.Fatalf("healthz: %d %q", resp.StatusCode, body)
	}
}

func TestFormatCell(t *testing.T) {
	cases := []struct {
		in   any
		want web.CellView
	}{
		{nil, web.CellView{Null: true}},
		{"", web.CellView{Text: ""}},
		{"NULL", web.CellView{Text: "NULL"}},
		{int64(42), web.CellView{Text: "42"}},
		{[]byte("halo"), web.CellView{Text: "halo"}},
		{[]byte{0xff, 0x00}, web.CellView{Text: `\xff00`}},
		{true, web.CellView{Text: "true"}},
	}
	for _, c := range cases {
		if got := web.FormatCellForTest(c.in); got != c.want {
			t.Errorf("formatCell(%#v) = %#v, ingin %#v", c.in, got, c.want)
		}
	}
	long := strings.Repeat("é", 600)
	if got := web.FormatCellForTest(long); len([]rune(got.Text)) != 501 {
		t.Errorf("teks panjang harus dipotong ke 500 karakter + elipsis, dapat %d", len([]rune(got.Text)))
	}
}
