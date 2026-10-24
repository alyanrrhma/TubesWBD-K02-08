package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
)

var ErrNotFound = errors.New("data tidak ditemukan")

type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

func inputErr(format string, a ...any) error {
	return &InputError{Message: fmt.Sprintf(format, a...)}
}

// SessionRevoker dipenuhi *auth.Manager (P1)
type SessionRevoker interface {
	RevokeUser(ctx context.Context, userID int64) error
}

type Service struct {
	db       *sql.DB
	registry *database.Registry
	sessions SessionRevoker
	box      *secretBox
}

// New membuat Service. registry boleh nil (koneksi tidak akan diaktifkan);
// sessions boleh nil (tidak ada pencabutan session saat user dihapus)
func New(db *sql.DB, registry *database.Registry, sessions SessionRevoker, secretKey []byte) (*Service, error) {
	if db == nil {
		return nil, errors.New("admin: platform db is nil")
	}
	box, err := newSecretBox(secretKey)
	if err != nil {
		return nil, err
	}
	return &Service{db: db, registry: registry, sessions: sessions, box: box}, nil
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)