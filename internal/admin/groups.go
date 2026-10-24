package admin

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
)

type Group struct {
	ID          int64
	Name        string
	MemberCount int
}

type GroupAccess struct {
	GroupID      int64
	DatabaseID   int64
	DatabaseName string
	Level        access.Level
}

func (s *Service) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.id, g.name, COUNT(ug.user_id)
		FROM app_groups g LEFT JOIN user_groups ug ON ug.group_id = g.id
		GROUP BY g.id, g.name ORDER BY lower(g.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Service) GetGroup(ctx context.Context, id int64) (*Group, error) {
	var g Group
	err := s.db.QueryRowContext(ctx, `SELECT g.id, g.name, (SELECT COUNT(*) FROM user_groups WHERE group_id = g.id)
		FROM app_groups g WHERE g.id = $1`, id).Scan(&g.ID, &g.Name, &g.MemberCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &g, err
}

func (s *Service) CreateGroup(ctx context.Context, name string) (*Group, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return nil, inputErr("Nama group wajib diisi (maksimal 64 karakter).")
	}
	var g Group
	err := s.db.QueryRowContext(ctx, `INSERT INTO app_groups (name) VALUES ($1) RETURNING id, name`, name).Scan(&g.ID, &g.Name)
	if pgCode(err) == pgUniqueViolation {
		return nil, inputErr("Nama group sudah dipakai.")
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *Service) DeleteGroup(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM app_groups WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ListMembers(ctx context.Context, groupID int64) ([]User, error) {
	if _, err := s.GetGroup(ctx, groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.username, u.is_platform_admin, u.created_at
		FROM users u JOIN user_groups ug ON ug.user_id = u.id
		WHERE ug.group_id = $1 ORDER BY lower(u.username)`, groupID)
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

func (s *Service) AddMember(ctx context.Context, groupID, userID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_groups (user_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, groupID)
	if pgCode(err) == pgForeignKeyViolation {
		return ErrNotFound
	}
	return err
}

func (s *Service) RemoveMember(ctx context.Context, groupID, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_groups WHERE user_id = $1 AND group_id = $2`, userID, groupID)
	return err
}

func (s *Service) ListGroupAccess(ctx context.Context, groupID int64) ([]GroupAccess, error) {
	if _, err := s.GetGroup(ctx, groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.group_id, a.database_id, c.name, a.access_level
		FROM group_database_access a JOIN database_connections c ON c.id = a.database_id
		WHERE a.group_id = $1 ORDER BY lower(c.name)`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupAccess
	for rows.Next() {
		var a GroupAccess
		var level string
		if err := rows.Scan(&a.GroupID, &a.DatabaseID, &a.DatabaseName, &level); err != nil {
			return nil, err
		}
		if a.Level, err = access.ParseLevel(level); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GrantAccess memberi (atau mengubah) level akses sebuah group ke sebuah database
func (s *Service) GrantAccess(ctx context.Context, groupID, databaseID int64, level access.Level) error {
	if level != access.ViewOnly && level != access.ViewEdit {
		return inputErr("Level akses tidak valid.")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO group_database_access (group_id, database_id, access_level) VALUES ($1, $2, $3)
		ON CONFLICT (group_id, database_id) DO UPDATE SET access_level = EXCLUDED.access_level`, groupID, databaseID, level.String())
	if pgCode(err) == pgForeignKeyViolation {
		return ErrNotFound
	}
	return err
}

func (s *Service) RevokeAccess(ctx context.Context, groupID, databaseID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM group_database_access WHERE group_id = $1 AND database_id = $2`, groupID, databaseID)
	return err
}