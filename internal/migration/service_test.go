package migration

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"github.com/kelompok/if3110-tubes/internal/database"
)

type fakeAuthz struct {
	view, edit bool
	err        error
}

func (f fakeAuthz) CanView(context.Context, int64, int64) (bool, error) { return f.view, f.err }
func (f fakeAuthz) CanEdit(context.Context, int64, int64) (bool, error) { return f.edit, f.err }

type fakeTargets struct {
	db  *sql.DB
	err error
}

func (f fakeTargets) Get(int64) (*sql.DB, error) { return f.db, f.err }

func TestExecute_RejectsBeforeTouchingDatabases(t *testing.T) {
	ctx := context.Background()
	editor := fakeAuthz{view: true, edit: true}

	cases := []struct {
		name  string
		svc   *Service
		uid   int64
		dbID  int64
		sql   string
		want  error
	}{
		{"user id invalid", NewService(nil, fakeTargets{}, editor), 0, 1, "SELECT 1", ErrInvalidArgument},
		{"empty sql", NewService(nil, fakeTargets{}, editor), 1, 1, "   ", ErrEmptySQL},
		{"too large", NewService(nil, fakeTargets{}, editor), 1, 1, strings.Repeat("a", maxSQLBytes+1), ErrSQLTooLarge},
		{"nul byte", NewService(nil, fakeTargets{}, editor), 1, 1, "SELECT '\x00'", ErrInvalidSQL},
		{"view only", NewService(nil, fakeTargets{}, fakeAuthz{view: true}), 1, 1, "SELECT 1", ErrForbidden},
		{"not registered", NewService(nil, fakeTargets{err: database.ErrNotRegistered}, editor), 1, 1, "SELECT 1", database.ErrNotRegistered},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.svc.Execute(ctx, c.uid, c.dbID, c.sql)
			if !errors.Is(err, c.want) {
				t.Fatalf("want %v, got %v", c.want, err)
			}
		})
	}
}

func TestHistory_ForbiddenAndInvalid(t *testing.T) {
	svc := NewService(nil, fakeTargets{}, fakeAuthz{}) // tidak punya akses sama sekali
	ctx := context.Background()

	if _, _, err := svc.ListHistory(ctx, 1, 1, 1, 10); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListHistory: got %v", err)
	}
	if _, err := svc.GetMigration(ctx, 1, 1, 1); !errors.Is(err, ErrForbidden) {
		t.Errorf("GetMigration: got %v", err)
	}
	if _, _, err := svc.ListHistory(ctx, 1, 1, 0, 10); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("page 0: got %v", err)
	}
	if _, _, err := svc.ListHistory(ctx, 1, 1, 1, 101); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("pageSize 101: got %v", err)
	}
}

func TestDescribeError_TruncatesAndDoesNotLeak(t *testing.T) {
	got := describeError(errors.New("dial tcp 10.0.0.5: password=s3cret"))
	if strings.Contains(got, "s3cret") || strings.Contains(got, "10.0.0.5") {
		t.Fatalf("leaked: %q", got)
	}
}