package access

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type repo struct{ grants []Grant }

func (r repo) GrantsForUser(context.Context, int64, int64) ([]Grant, error) { return r.grants, nil }
func TestEffectiveAccess(t *testing.T) {
	for _, tc := range []struct {
		g    []Grant
		want Level
	}{{nil, NoAccess}, {[]Grant{{GroupID: 1, DatabaseID: 3, Level: ViewOnly}}, ViewOnly}, {[]Grant{{GroupID: 1, DatabaseID: 3, Level: ViewOnly}, {GroupID: 2, DatabaseID: 3, Level: ViewEdit}}, ViewEdit}} {
		s := Service{repo{tc.g}}
		got, e := s.EffectiveAccess(context.Background(), 1, 3)
		if e != nil || got != tc.want {
			t.Fatalf("got %v %v", got, e)
		}
	}
}
func TestDirectRequestForbidden(t *testing.T) {
	s := Service{repo{[]Grant{{GroupID: 1, DatabaseID: 3, Level: ViewOnly}}}}
	called := false
	h := s.Middleware(ViewEdit, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
	r := httptest.NewRequest("POST", "http://localhost/databases/3/migrations", nil)
	r.SetPathValue("databaseID", "3")
	r = r.WithContext(WithUserID(r.Context(), 1))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || called {
		t.Fatal("edit permitted for view-only")
	}
	if !errors.Is(s.Require(context.Background(), 1, 3, ViewEdit), ErrForbidden) {
		t.Fatal("not forbidden")
	}
}

func TestEffectiveAccessDifferentDatabase(t *testing.T) {
	s := Service{repo{[]Grant{
		{GroupID: 1, DatabaseID: 99, Level: ViewEdit},
	}}}

	got, err := s.EffectiveAccess(context.Background(), 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got != NoAccess {
		t.Fatalf("got %v, want NoAccess", got)
	}
}

func TestEffectiveAccessInvalidIDs(t *testing.T) {
	s := Service{repo{}}

	for _, tc := range []struct {
		userID, databaseID int64
	}{
		{0, 3},
		{-1, 3},
		{1, 0},
		{1, -1},
	} {
		_, err := s.EffectiveAccess(
			context.Background(), tc.userID, tc.databaseID,
		)
		if err == nil {
			t.Fatalf("expected error for IDs %v", tc)
		}
	}
}

func TestEffectiveAccessRepositoryError(t *testing.T) {
	s := Service{
		Repo: errorRepo{},
	}

	_, err := s.EffectiveAccess(context.Background(), 1, 3)
	if err == nil {
		t.Fatal("expected repository error")
	}
}

type errorRepo struct{}

func (errorRepo) GrantsForUser(
	context.Context, int64, int64,
) ([]Grant, error) {
	return nil, errors.New("database unavailable")
}

func TestEffectiveAccessNilRepository(t *testing.T) {
	s := Service{}

	_, err := s.EffectiveAccess(context.Background(), 1, 3)
	if err == nil {
		t.Fatal("expected error for nil repository")
	}
}

func TestRequireViewOnlyCannotEdit(t *testing.T) {
	s := Service{
		Repo: repo{
			grants: []Grant{
				{
					GroupID:    1,
					DatabaseID: 3,
					Level:      ViewOnly,
				},
			},
		},
	}

	err := s.Require(context.Background(), 1, 3, ViewEdit)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestMiddlewareMissingUser(t *testing.T) {
	s := Service{
		Repo: repo{},
	}

	called := false
	h := s.Middleware(
		ViewOnly,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}),
	)

	r := httptest.NewRequest(
		http.MethodGet,
		"/databases/3",
		nil,
	)
	r.SetPathValue("databaseID", "3")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if called {
		t.Fatal("handler should not be called without a user")
	}
}

func TestMiddlewareInvalidDatabaseID(t *testing.T) {
	s := Service{
		Repo: repo{},
	}

	called := false
	h := s.Middleware(
		ViewOnly,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}),
	)

	r := httptest.NewRequest(
		http.MethodGet,
		"/databases/invalid",
		nil,
	)
	r.SetPathValue("databaseID", "invalid")
	r = r.WithContext(WithUserID(r.Context(), 1))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if called {
		t.Fatal("handler should not be called for invalid database ID")
	}
}

func TestMiddlewareRepositoryError(t *testing.T) {
	s := Service{
		Repo: errorRepo{},
	}

	called := false
	h := s.Middleware(
		ViewOnly,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}),
	)

	r := httptest.NewRequest(
		http.MethodGet,
		"/databases/3",
		nil,
	)
	r.SetPathValue("databaseID", "3")
	r = r.WithContext(WithUserID(r.Context(), 1))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if called {
		t.Fatal("handler should not be called after repository error")
	}
}
