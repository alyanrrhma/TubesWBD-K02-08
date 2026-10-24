package web_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/admin"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/migration"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/web"
)

// Fake sederhana agar test handler tidak membutuhkan PostgreSQL. Alur dengan database nyata ada di tests/integration.

type fakeUsers struct {
	mu    sync.Mutex
	next  int64
	users map[string]*fakeUser
}

type fakeUser struct {
	admin.User
	hash string
}

func newFakeUsers(hash string) *fakeUsers {
	f := &fakeUsers{users: map[string]*fakeUser{}}
	for _, u := range []struct {
		name  string
		admin bool
	}{{"admin", true}, {"alice", false}, {"bob", false}} {
		f.next++
		f.users[u.name] = &fakeUser{User: admin.User{ID: f.next, Username: u.name, IsAdmin: u.admin, CreatedAt: time.Now()}, hash: hash}
	}
	return f
}

// auth.UserRepository
func (f *fakeUsers) FindByUsername(_ context.Context, name string) (auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[name]
	if !ok {
		return auth.User{}, sql.ErrNoRows
	}
	return auth.User{ID: u.ID, PasswordHash: u.hash, Admin: u.IsAdmin}, nil
}

func (f *fakeUsers) id(name string) int64 { return f.users[name].ID }

// web.AdminService: hanya method yang dipakai test; sisanya nil -> panic bila terpanggil.
type fakeAdmin struct {
	web.AdminService
	users  *fakeUsers
	groups []admin.Group
}

func (a *fakeAdmin) GetUser(_ context.Context, id int64) (*admin.User, error) {
	a.users.mu.Lock()
	defer a.users.mu.Unlock()
	for _, u := range a.users.users {
		if u.ID == id {
			cp := u.User
			return &cp, nil
		}
	}
	return nil, admin.ErrNotFound
}

func (a *fakeAdmin) ListUsers(context.Context) ([]admin.User, error) {
	a.users.mu.Lock()
	defer a.users.mu.Unlock()
	var out []admin.User
	for _, u := range a.users.users {
		out = append(out, u.User)
	}
	return out, nil
}

func (a *fakeAdmin) CreateUser(_ context.Context, name, _ string, isAdmin bool) (*admin.User, error) {
	a.users.mu.Lock()
	defer a.users.mu.Unlock()
	if _, ok := a.users.users[name]; ok {
		return nil, &admin.InputError{Message: "Username sudah dipakai."}
	}
	a.users.next++
	u := &fakeUser{User: admin.User{ID: a.users.next, Username: name, IsAdmin: isAdmin, CreatedAt: time.Now()}}
	a.users.users[name] = u
	return &u.User, nil
}

func (a *fakeAdmin) DeleteUser(context.Context, int64) error { return nil }

func (a *fakeAdmin) ListGroups(context.Context) ([]admin.Group, error) { return a.groups, nil }

func (a *fakeAdmin) CreateGroup(_ context.Context, name string) (*admin.Group, error) {
	g := admin.Group{ID: int64(len(a.groups) + 1), Name: name}
	a.groups = append(a.groups, g)
	return &g, nil
}

// fakeAccess menggantikan access.Service dalam unit test viewer dan migration.
type fakeAccess struct{ levels map[[2]int64]access.Level }

func (f fakeAccess) EffectiveAccess(_ context.Context, u, d int64) (access.Level, error) {
	return f.levels[[2]int64{u, d}], nil
}

func (f fakeAccess) Require(ctx context.Context, u, d int64, needed access.Level) error {
	l, _ := f.EffectiveAccess(ctx, u, d)
	if l < needed || l == access.NoAccess {
		return access.ErrForbidden
	}
	return nil
}

// fakeViewer memakai web.DatabaseViewer sungguhan di atas fake registry? Tidak: registry butuh *sql.DB.
// Karena itu viewer di-fake langsung dan memeriksa akses lewat fakeAccess.
type fakeViewer struct {
	access  fakeAccess
	dbs     map[int64]web.DatabaseView
	err     error // bila diisi, semua operasi data mengembalikannya
	rowsFor func(page int) *web.TableDataView
}

func (v *fakeViewer) level(ctx context.Context, u, d int64) (*web.DatabaseView, error) {
	l, _ := v.access.EffectiveAccess(ctx, u, d)
	db, ok := v.dbs[d]
	if l == access.NoAccess || !ok {
		return nil, access.ErrForbidden
	}
	db.Level = l
	return &db, nil
}

func (v *fakeViewer) ListDatabases(ctx context.Context, u int64) ([]web.DatabaseView, error) {
	var out []web.DatabaseView
	for id := range v.dbs {
		if db, err := v.level(ctx, u, id); err == nil {
			out = append(out, *db)
		}
	}
	return out, nil
}

func (v *fakeViewer) Database(ctx context.Context, u, d int64) (*web.DatabaseView, error) {
	return v.level(ctx, u, d)
}

func (v *fakeViewer) data(ctx context.Context, u, d int64) error {
	if _, err := v.level(ctx, u, d); err != nil {
		return err
	}
	return v.err
}

func (v *fakeViewer) Schemas(ctx context.Context, u, d int64) ([]string, error) {
	return []string{"audit", "public"}, v.data(ctx, u, d)
}

func (v *fakeViewer) Tables(ctx context.Context, u, d int64, schema string) ([]database.Table, error) {
	return []database.Table{{Schema: schema, Name: "customers"}}, v.data(ctx, u, d)
}

func (v *fakeViewer) TableDetail(ctx context.Context, u, d int64, schema, table string) (*web.TableDetailView, error) {
	if err := v.data(ctx, u, d); err != nil {
		return nil, err
	}
	return &web.TableDetailView{
		Columns:     []database.Column{{Name: "id", DataType: "integer", Position: 1}, {Name: "email", DataType: "text", IsNullable: true, Position: 2}},
		Constraints: []database.Constraint{{Name: "customers_pkey", Type: "PRIMARY KEY", Definition: "PRIMARY KEY (id)"}},
		Indexes:     []database.Index{{Name: "customers_pkey", Definition: "CREATE UNIQUE INDEX ...", IsUnique: true, IsPrimary: true}},
	}, nil
}

func (v *fakeViewer) TableData(ctx context.Context, u, d int64, schema, table string, page, size int) (*web.TableDataView, error) {
	if err := v.data(ctx, u, d); err != nil {
		return nil, err
	}
	return v.rowsFor(page), nil
}

// recordingMigrations meniru modul P2: View Edit wajib, SQL berisi "fail" -> record FAILED.
type recordingMigrations struct {
	access  fakeAccess
	history []web.MigrationRecord
}

func (m *recordingMigrations) History(ctx context.Context, u, d int64, page, pageSize int) ([]web.MigrationRecord, int, error) {
	if err := m.access.Require(ctx, u, d, access.ViewOnly); err != nil {
		return nil, 0, err
	}
	total := len(m.history)
	start := (page - 1) * pageSize
	if start >= total {
		return nil, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return m.history[start:end], total, nil
}

func (m *recordingMigrations) Execute(ctx context.Context, u, d int64, sql string) (*web.MigrationRecord, error) {
	if err := m.access.Require(ctx, u, d, access.ViewEdit); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sql) == "" {
		return nil, migration.ErrEmptySQL
	}
	rec := web.MigrationRecord{ID: int64(len(m.history) + 1), SQL: sql, ExecutedBy: "tester", ExecutedAt: time.Now()}
	if strings.Contains(strings.ToLower(sql), "fail") {
		rec.Status, rec.ErrorMessage = web.MigrationFailed, "syntax error at or near \"fail\""
	} else {
		rec.Status, rec.Version = web.MigrationSuccess, len(m.history)+1
	}
	m.history = append([]web.MigrationRecord{rec}, m.history...)
	return &rec, nil
}