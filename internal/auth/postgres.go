package auth

import (
	"context"
	"database/sql"
	"errors"
)

// PostgreSQL-backed store. An injected database/sql driver allows teams to use pgx.
type PostgresStore struct{ DB *sql.DB }

func (p PostgresStore) Create(ctx context.Context, s Session) error {
	if p.DB == nil {
		return errors.New("session store: database is not configured")
	}
	_, e := p.DB.ExecContext(ctx, `INSERT INTO sessions(family_id,user_id,is_admin,token_hash,csrf_hash,created_at,last_seen,rotated_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.FamilyID, s.Principal.UserID, s.Principal.Admin, s.TokenHash[:], s.CSRFHash[:], s.CreatedAt, s.LastSeen, s.RotatedAt, s.ExpiresAt)
	return e
}
func (p PostgresStore) FindByTokenHash(ctx context.Context, h [32]byte) (Session, error) {
	if p.DB == nil {
		return Session{}, errors.New("session store: database is not configured")
	}
	var s Session
	var current, old, csrf []byte
	var oldExp sql.NullTime
	e := p.DB.QueryRowContext(ctx, `SELECT family_id,user_id,is_admin,token_hash,old_token_hash,old_expires_at,csrf_hash,created_at,last_seen,rotated_at,expires_at,revoked FROM sessions WHERE token_hash=$1 OR (old_token_hash=$1 AND old_expires_at > NOW())`, h[:]).Scan(&s.FamilyID, &s.Principal.UserID, &s.Principal.Admin, &current, &old, &oldExp, &csrf, &s.CreatedAt, &s.LastSeen, &s.RotatedAt, &s.ExpiresAt, &s.Revoked)
	if e != nil {
		return Session{}, e
	}
	if len(current) != 32 || len(csrf) != 32 {
		return Session{}, errors.New("corrupt session")
	}
	copy(s.TokenHash[:], current)
	copy(s.CSRFHash[:], csrf)
	if len(old) == 32 {
		copy(s.OldTokenHash[:], old)
	}
	if oldExp.Valid {
		s.OldExpiresAt = oldExp.Time
	}
	return s, nil
}
func (p PostgresStore) Save(ctx context.Context, s Session) error {
	if p.DB == nil {
		return errors.New("session store: database is not configured")
	}
	var old interface{}
	var oldExp interface{}
	if !s.OldExpiresAt.IsZero() {
		old = s.OldTokenHash[:]
		oldExp = s.OldExpiresAt
	}
	res, e := p.DB.ExecContext(ctx, `UPDATE sessions SET is_admin=$2,token_hash=$3,old_token_hash=$4,old_expires_at=$5,csrf_hash=$6,last_seen=$7,rotated_at=$8,revoked=$9 WHERE family_id=$1 AND revoked=FALSE`, s.FamilyID, s.Principal.Admin, s.TokenHash[:], old, oldExp, s.CSRFHash[:], s.LastSeen, s.RotatedAt, s.Revoked)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
func (p PostgresStore) CompareAndSwap(
	ctx context.Context,
	s Session,
	expectedTokenHash [32]byte,
) error {
	if p.DB == nil {
		return errors.New("session store: database is not configured")
	}

	var old interface{}
	var oldExp interface{}

	if !s.OldExpiresAt.IsZero() {
		old = s.OldTokenHash[:]
		oldExp = s.OldExpiresAt
	}

	res, err := p.DB.ExecContext(ctx, `
        UPDATE sessions
        SET user_id = $2,
            is_admin = $3,
            token_hash = $4,
            old_token_hash = $5,
            old_expires_at = $6,
            csrf_hash = $7,
            last_seen = $8,
            rotated_at = $9,
            revoked = $10
        WHERE family_id = $1
          AND token_hash = $11
          AND revoked = FALSE
    `,
		s.FamilyID,
		s.Principal.UserID,
		s.Principal.Admin,
		s.TokenHash[:],
		old,
		oldExp,
		s.CSRFHash[:],
		s.LastSeen,
		s.RotatedAt,
		s.Revoked,
		expectedTokenHash[:],
	)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if rows != 1 {
		return ErrSessionConflict
	}

	return nil
}
func (p PostgresStore) RevokeFamily(ctx context.Context, family string) error {
	if p.DB == nil {
		return errors.New("session store: database is not configured")
	}
	_, e := p.DB.ExecContext(ctx, `UPDATE sessions SET revoked=TRUE WHERE family_id=$1`, family)
	return e
}
func (p PostgresStore) RevokeUser(ctx context.Context, id int64) error {
	if p.DB == nil {
		return errors.New("session store: database is not configured")
	}
	_, e := p.DB.ExecContext(ctx, `UPDATE sessions SET revoked=TRUE WHERE user_id=$1`, id)
	return e
}