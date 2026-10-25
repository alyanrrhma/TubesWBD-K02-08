package access

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

type Level int

const (
	NoAccess Level = iota
	ViewOnly
	ViewEdit
)

func ParseLevel(s string) (Level, error) {
	switch s {
	case "VIEW_ONLY":
		return ViewOnly, nil
	case "VIEW_EDIT":
		return ViewEdit, nil
	}
	return NoAccess, errors.New("unknown access level")
}
func (l Level) String() string {
	switch l {
	case ViewOnly:
		return "VIEW_ONLY"
	case ViewEdit:
		return "VIEW_EDIT"
	}
	return "NO_ACCESS"
}

type Grant struct {
	GroupID, DatabaseID int64
	Level               Level
}
type Repository interface {
	GrantsForUser(ctx context.Context, userID, databaseID int64) ([]Grant, error)
}
type Service struct{ Repo Repository }

// EffectiveAccess uses highest grant from any group (documented course-project policy).
func (s Service) EffectiveAccess(
	ctx context.Context,
	userID, databaseID int64,
) (Level, error) {
	if userID <= 0 || databaseID <= 0 {
		return NoAccess, errors.New("invalid id")
	}

	if s.Repo == nil {
		return NoAccess, ErrInvalidService
	}

	grants, e := s.Repo.GrantsForUser(ctx, userID, databaseID)
	if e != nil {
		return NoAccess, e
	}

	level := NoAccess
	for _, g := range grants {
		if g.DatabaseID == databaseID &&
			g.Level > level &&
			g.Level <= ViewEdit {
			level = g.Level
		}
	}

	return level, nil
}
func (s Service) Require(ctx context.Context, userID, databaseID int64, needed Level) error {
	if needed != ViewOnly && needed != ViewEdit {
		return ErrInvalidLevel
	}
	level, e := s.EffectiveAccess(ctx, userID, databaseID)
	if e != nil {
		return e
	}
	if level < needed {
		return ErrForbidden
	}
	return nil
}

var (
	ErrForbidden      = errors.New("forbidden")
	ErrInvalidLevel   = errors.New("invalid access level")
	ErrInvalidService = errors.New("access service is not configured")
)

type UserIDContextKey struct{}

func UserID(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(UserIDContextKey{}).(int64)
	return v, ok
}
func WithUserID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, UserIDContextKey{}, id)
}

// Middleware must wrap every DB viewer and migration route; protects direct HTTP calls.
func (s Service) Middleware(needed Level, next http.Handler) http.Handler {
	if needed != ViewOnly && needed != ViewEdit {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "invalid access configuration",
				http.StatusInternalServerError)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := UserID(r.Context())
		if !ok || id <= 0 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		dbID, e := strconv.ParseInt(r.PathValue("databaseID"), 10, 64)
		if e != nil || dbID <= 0 {
			http.Error(w, "invalid database", http.StatusBadRequest)
			return
		}
		if e = s.Require(r.Context(), id, dbID, needed); e != nil {
			if errors.Is(e, ErrForbidden) {
				http.Error(w, "forbidden", http.StatusForbidden)
			} else {
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}