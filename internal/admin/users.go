package admin

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
)

type User struct {
	ID        int64
	Username  string
	IsAdmin   bool
	CreatedAt time.Time
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,64}$`)

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, is_platform_admin, created_at FROM users ORDER BY lower(username)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Service) GetUser(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT id, username, is_platform_admin, created_at FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

// CreateUser menyimpan akun baru; password di-hash dengan auth.HashPassword (P1).
func (s *Service) CreateUser(ctx context.Context, username, password string, isAdmin bool) (*User, error) {
	username = strings.TrimSpace(username)
	if !usernamePattern.MatchString(username) {
		return nil, inputErr("Username harus 3–64 karakter: huruf, angka, titik, garis bawah, atau tanda hubung.")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, inputErr("Password harus berisi 8–1024 karakter.")
	}
	var u User
	err = s.db.QueryRowContext(ctx, `INSERT INTO users (username, password_hash, is_platform_admin) VALUES ($1, $2, $3)
		RETURNING id, username, is_platform_admin, created_at`, username, hash, isAdmin).
		Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt)
	if pgCode(err) == pgUniqueViolation {
		return nil, inputErr("Username sudah dipakai.")
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// DeleteUser menghapus akun dan seluruh session-nya. Platform Admin terakhir tidak boleh dihapus.
func (s *Service) DeleteUser(ctx context.Context, id int64) error {
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if u.IsAdmin {
		var others int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_platform_admin AND id <> $1`, id).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return inputErr("Platform Admin terakhir tidak dapat dihapus.")
		}
	}
	if s.sessions != nil {
		if err := s.sessions.RevokeUser(ctx, id); err != nil {
			return err
		}
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// EnsureAdmin membuat Platform Admin pertama bila belum ada satu pun
// Mengembalikan true jika akun dibuat.
func (s *Service) EnsureAdmin(ctx context.Context, username, password string) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE is_platform_admin)`).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if _, err := s.CreateUser(ctx, username, password, true); err != nil {
		return false, err
	}
	return true, nil
}