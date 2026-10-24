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
