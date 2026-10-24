package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	maxPageSize  = 100
	queryTimeout = 15 * time.Second
)

// requireView memastikan user boleh melihat database tersebut (View Only atau View+Edit).
// Dicek di backend pada setiap pemanggilan, sehingga pencabutan akses langsung berlaku.
func (s *Service) requireView(ctx context.Context, userID, databaseID int64) error {
	if userID <= 0 || databaseID <= 0 {
		return fmt.Errorf("%w: user and database ID must be positive", ErrInvalidArgument)
	}
	ok, err := s.authz.CanView(ctx, userID, databaseID)
	if err != nil {
		return fmt.Errorf("check view access: %w", err)
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

// ListHistory mengembalikan riwayat migration satu database (terbaru dulu) beserta
// total baris. Page dimulai dari 1. SQLText sengaja dikosongkan pada daftar;
// ambil lewat GetMigration.
func (s *Service) ListHistory(ctx context.Context, userID, databaseID int64, page, pageSize int) ([]Migration, int, error) {
	if page < 1 {
		return nil, 0, fmt.Errorf("%w: page must be at least 1", ErrInvalidArgument)
	}
	if pageSize < 1 || pageSize > maxPageSize {
		return nil, 0, fmt.Errorf("%w: page size must be between 1 and %d", ErrInvalidArgument, maxPageSize)
	}
	// Otorisasi dulu, baru menyentuh data.
	if err := s.requireView(ctx, userID, databaseID); err != nil {
		return nil, 0, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var total int
	err := s.platform.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM migration_history WHERE database_id = $1`,
		databaseID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count migration history: %w", err)
	}

	rows, err := s.platform.QueryContext(ctx, `
		SELECT m.id, m.database_id, m.version, m.executed_by, u.username,
		       m.executed_at, m.status, COALESCE(m.error_message, '')
		FROM migration_history m
		JOIN users u ON u.id = m.executed_by
		WHERE m.database_id = $1
		ORDER BY m.version DESC
		LIMIT $2 OFFSET $3
	`, databaseID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list migration history: %w", err)
	}
	defer rows.Close()

	items := make([]Migration, 0, pageSize)
	for rows.Next() {
		var m Migration
		var status string
		if err := rows.Scan(
			&m.ID, &m.DatabaseID, &m.Version, &m.ExecutedBy, &m.ExecutedByName,
			&m.ExecutedAt, &status, &m.ErrorMessage,
		); err != nil {
			return nil, 0, fmt.Errorf("scan migration history: %w", err)
		}
		m.Status = Status(status)
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate migration history: %w", err)
	}
	return items, total, nil
}

// GetMigration mengembalikan detail satu migration, termasuk SQL yang dijalankan.
func (s *Service) GetMigration(ctx context.Context, userID, databaseID, version int64) (Migration, error) {
	if version < 1 {
		return Migration{}, fmt.Errorf("%w: version must be positive", ErrInvalidArgument)
	}
	// Forbidden dicek sebelum NotFound agar user tanpa hak tidak bisa
	// menebak versi mana yang ada.
	if err := s.requireView(ctx, userID, databaseID); err != nil {
		return Migration{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var m Migration
	var status string
	err := s.platform.QueryRowContext(ctx, `
		SELECT m.id, m.database_id, m.version, m.sql_text, m.executed_by, u.username,
		       m.executed_at, m.status, COALESCE(m.error_message, '')
		FROM migration_history m
		JOIN users u ON u.id = m.executed_by
		WHERE m.database_id = $1 AND m.version = $2
	`, databaseID, version).Scan(
		&m.ID, &m.DatabaseID, &m.Version, &m.SQLText, &m.ExecutedBy, &m.ExecutedByName,
		&m.ExecutedAt, &status, &m.ErrorMessage,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Migration{}, ErrMigrationNotFound
	}
	if err != nil {
		return Migration{}, fmt.Errorf("get migration: %w", err)
	}
	m.Status = Status(status)
	return m, nil
}