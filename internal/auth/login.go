package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
)

type User struct {
	ID           int64
	PasswordHash string
	Admin        bool
}
type UserRepository interface {
	FindByUsername(context.Context, string) (User, error)
}
type PostgresUsers struct{ DB *sql.DB }

func (p PostgresUsers) FindByUsername(ctx context.Context, username string) (User, error) {
	var u User
	e := p.DB.QueryRowContext(ctx, `SELECT id,password_hash,is_platform_admin FROM users WHERE username=$1`, username).Scan(&u.ID, &u.PasswordHash, &u.Admin)
	return u, e
}

type LoginService struct {
	Users    UserRepository
	Sessions *Manager
}

var ErrCredentials = errors.New("invalid credentials")

func (s LoginService) Login(ctx context.Context, w http.ResponseWriter, username, password string) (string, error) {
	u, e := s.Users.FindByUsername(ctx, username)
	if e != nil {
		return "", ErrCredentials
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return "", ErrCredentials
	}
	// Existing cookie is revoked to prevent session fixation / parallel login reuse.
	if c, e := (&http.Request{Header: http.Header{}}).Cookie(CookieName); e == nil {
		_ = c
	} // no request here; handler must call RevokeCurrent first
	return s.Sessions.Login(ctx, w, Principal{UserID: u.ID, Admin: u.Admin})
}

// RevokeCurrent should be used by login HTTP handler before issuing a replacement.
func (s LoginService) RevokeCurrent(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	old, _, e := s.Sessions.Validate(ctx, w, r)
	if e == nil {
		_ = s.Sessions.Logout(ctx, w, old)
	}
}
