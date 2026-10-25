package migration

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func openTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(u.Path, "/"), "_test") {
		t.Fatalf("test DSN must point to a database whose name ends with _test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	return db
}

// setup mengembalikan service dengan akses penuh, plus db target dan IDs.
func setup(t *testing.T, authz Authorizer) (svc *Service, target *sql.DB, userID, dbID int64) {
	t.Helper()
	pDSN, tDSN := os.Getenv("TEST_PLATFORM_DATABASE_URL"), os.Getenv("TEST_TARGET_DATABASE_URL")
	if pDSN == "" || tDSN == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL / TEST_TARGET_DATABASE_URL not set")
	}

	platform := openTestDB(t, pDSN)
	target = openTestDB(t, tDSN)

	schema, err := os.ReadFile("../platformdb/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if err := platform.QueryRow(
		`INSERT INTO users(username, password_hash) VALUES ('tester','x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := platform.QueryRow(
		`INSERT INTO database_connections(name) VALUES ('target') RETURNING id`).Scan(&dbID); err != nil {
		t.Fatal(err)
	}
	if authz == nil {
		authz = fakeAuthz{view: true, edit: true}
	}
	return NewService(platform, fakeTargets{db: target}, authz), target, userID, dbID
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var ok bool
	if err := db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestExecute_Success(t *testing.T) {
	svc, target, uid, dbID := setup(t, nil)
	rec, err := svc.Execute(context.Background(), uid, dbID, "CREATE TABLE t1(id int)")
	if err != nil || rec.Status != StatusSuccess || rec.Version != 1 {
		t.Fatalf("rec=%+v err=%v", rec, err)
	}
	if !tableExists(t, target, "public.t1") {
		t.Fatal("table was not created in target")
	}
}

// Sekaligus membuktikan multi-statement berjalan lewat satu ExecContext.
func TestExecute_FailureRollsBackEverything(t *testing.T) {
	svc, target, uid, dbID := setup(t, nil)
	rec, err := svc.Execute(context.Background(), uid, dbID,
		"CREATE TABLE a(id int); INSERT INTO table_tidak_ada VALUES (1);")
	if err != nil {
		t.Fatalf("failed migration must not be a Go error, got %v", err)
	}
	if rec.Status != StatusFailed || rec.ErrorMessage == "" {
		t.Fatalf("rec=%+v", rec)
	}
	if tableExists(t, target, "public.a") {
		t.Fatal("first statement leaked despite rollback")
	}
}

func TestExecute_VersionsAreLinearUnderConcurrency(t *testing.T) {
	svc, _, uid, dbID := setup(t, nil)
	const n = 10

	versions := make(chan int64, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, err := svc.Execute(context.Background(), uid, dbID, "SELECT 1")
			if err != nil {
				t.Errorf("execute: %v", err)
				return
			}
			versions <- rec.Version
		}()
	}
	wg.Wait()
	close(versions)

	seen := map[int64]bool{}
	for v := range versions {
		if seen[v] {
			t.Fatalf("duplicate version %d", v)
		}
		seen[v] = true
	}
	for v := int64(1); v <= n; v++ {
		if !seen[v] {
			t.Fatalf("gap: version %d missing", v)
		}
	}

	// Migration gagal juga memakai nomor versi berikutnya.
	rec, _ := svc.Execute(context.Background(), uid, dbID, "SELECT * FROM nope")
	if rec.Version != n+1 || rec.Status != StatusFailed {
		t.Fatalf("rec=%+v", rec)
	}
}

func TestViewOnly_CanReadHistoryButNotExecute(t *testing.T) {
	svc, _, uid, dbID := setup(t, nil)
	if _, err := svc.Execute(context.Background(), uid, dbID, "SELECT 1"); err != nil {
		t.Fatal(err)
	}

	viewOnly := NewService(svc.platform, svc.targets, fakeAuthz{view: true})
	if _, err := viewOnly.Execute(context.Background(), uid, dbID, "SELECT 2"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
	items, total, err := viewOnly.ListHistory(context.Background(), uid, dbID, 1, 10)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("total=%d items=%d err=%v", total, len(items), err)
	}
	if _, err := viewOnly.GetMigration(context.Background(), uid, dbID, 99); !errors.Is(err, ErrMigrationNotFound) {
		t.Fatalf("want ErrMigrationNotFound, got %v", err)
	}
}
func TestHistory_OrderPaginationAndDetail(t *testing.T) {
	svc, _, uid, dbID := setup(t, nil)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := svc.Execute(ctx, uid, dbID, "SELECT 1"); err != nil {
			t.Fatal(err)
		}
	}

	items, total, err := svc.ListHistory(ctx, uid, dbID, 1, 2)
	if err != nil || total != 5 || len(items) != 2 {
		t.Fatalf("total=%d items=%d err=%v", total, len(items), err)
	}
	if items[0].Version != 5 || items[1].Version != 4 { // terbaru dulu
		t.Fatalf("order wrong: %d, %d", items[0].Version, items[1].Version)
	}
	if items[0].SQLText != "" {
		t.Fatal("list must not include sql_text")
	}

	last, _, err := svc.ListHistory(ctx, uid, dbID, 3, 2)
	if err != nil || len(last) != 1 || last[0].Version != 1 {
		t.Fatalf("last page wrong: %+v err=%v", last, err)
	}

	m, err := svc.GetMigration(ctx, uid, dbID, 3)
	if err != nil || m.SQLText != "SELECT 1" || m.ExecutedByName != "tester" {
		t.Fatalf("detail wrong: %+v err=%v", m, err)
	}
}
func TestRealAccess_Matrix(t *testing.T) {
	svc, _, _, dbID := setup(t, nil)
	ctx := context.Background()
	p := svc.platform

	newUser := func(name string, admin bool) int64 {
		var id int64
		if err := p.QueryRow(
			`INSERT INTO users(username,password_hash,is_platform_admin) VALUES ($1,'x',$2) RETURNING id`,
			name, admin).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newGroup := func(name, level string) int64 {
		var id int64
		if err := p.QueryRow(`INSERT INTO app_groups(name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Exec(
			`INSERT INTO group_database_access(group_id,database_id,access_level) VALUES ($1,$2,$3)`,
			id, dbID, level); err != nil {
			t.Fatal(err)
		}
		return id
	}
	join := func(uid, gid int64) {
		if _, err := p.Exec(`INSERT INTO user_groups(user_id,group_id) VALUES ($1,$2)`, uid, gid); err != nil {
			t.Fatal(err)
		}
	}

	viewGroup := newGroup("viewers", "VIEW_ONLY")
	editGroup := newGroup("editors", "VIEW_EDIT")

	viewer := newUser("viewer", false)
	editor := newUser("editor", false)
	both := newUser("both", false)
	outsider := newUser("outsider", false)
	admin := newUser("admin", true) // platform admin, tidak di group mana pun

	join(viewer, viewGroup)
	join(editor, editGroup)
	join(both, viewGroup)
	join(both, editGroup)

	real := NewService(p, svc.targets, AccessAuthorizer{
		Access: access.Service{Repo: access.PostgresRepository{DB: p}},
	})

	cases := []struct {
		name             string
		uid              int64
		canView, canEdit bool
	}{
		{"view only", viewer, true, false},
		{"view edit", editor, true, true},
		{"two groups: highest wins", both, true, true},
		{"no group", outsider, false, false},
		{"platform admin is not a database level", admin, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, errV := real.ListHistory(ctx, c.uid, dbID, 1, 10)
			if (errV == nil) != c.canView || (errV != nil && !errors.Is(errV, ErrForbidden)) {
				t.Fatalf("ListHistory: canView=%v err=%v", c.canView, errV)
			}
			_, errE := real.Execute(ctx, c.uid, dbID, "SELECT 1")
			if (errE == nil) != c.canEdit || (errE != nil && !errors.Is(errE, ErrForbidden)) {
				t.Fatalf("Execute: canEdit=%v err=%v", c.canEdit, errE)
			}
		})
	}

	// Pencabutan akses harus langsung berlaku.
	if _, err := p.Exec(`DELETE FROM group_database_access WHERE group_id=$1`, editGroup); err != nil {
		t.Fatal(err)
	}
	if _, err := real.Execute(ctx, editor, dbID, "SELECT 1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("after revoke: want ErrForbidden, got %v", err)
	}
}