package migration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxSQLBytes        = 1 << 20 // 1 MiB
	operationTimeout   = 2 * time.Minute
	maxErrorMessageLen = 2000
)

func NewService(platform *sql.DB, targets Targets, authz Authorizer) *Service {
	return &Service{platform: platform, targets: targets, authz: authz}
}

func (s *Service) Execute(ctx context.Context, userID, databaseID int64, sqlText string) (Migration, error) {
	if userID <= 0 || databaseID <= 0 {
		return Migration{}, fmt.Errorf("%w: user and database ID must be positive", ErrInvalidArgument)
	}

	// Otorisasi harus dicek sebelum memvalidasi SQL atau menyentuh database agar
	// pengguna tanpa izin tidak dapat memakai endpoint ini untuk menguji input.
	ok, err := s.authz.CanEdit(ctx, userID, databaseID)
	if err != nil {
		return Migration{}, fmt.Errorf("check edit access: %w", err)
	}
	if !ok {
		return Migration{}, ErrForbidden
	}

	sqlText = strings.TrimSpace(sqlText)
	switch {
	case sqlText == "":
		return Migration{}, ErrEmptySQL
	case len(sqlText) > maxSQLBytes:
		return Migration{}, ErrSQLTooLarge
	case strings.ContainsRune(sqlText, 0): // NUL di kolom text
		return Migration{}, ErrInvalidSQL
	}

	target, err := s.targets.Get(databaseID)
	if err != nil {
		return Migration{}, err
	}

	// Satu migration per database pada satu waktu.
	mu := s.lockFor(databaseID)
	mu.Lock()
	defer mu.Unlock()

	// Context dilepas dari context request: kalau client putus setelah target ter-commit, riwayat tetap harus tercatat.
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), operationTimeout)
	defer cancel()

	var version int64
	// Cari untuk menentukan nomor versi migration berikutnya
	err = s.platform.QueryRowContext(opCtx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM migration_history WHERE database_id = $1`, databaseID).Scan(&version)
	if err != nil {
		return Migration{}, fmt.Errorf("next migration version: %w", err)
	}

	tx, err := target.BeginTx(opCtx, nil)
	if err != nil {
		// Belum ada SQL yang dijalankan, jadi tidak dicatat sebagai riwayat.
		return Migration{}, fmt.Errorf("%w: begin transaction: %v", database.ErrConnectFailed, err)
	}
	applyErr := applyInTx(opCtx, tx, sqlText)
	// Susun riwayat berdasarkan hasil transaksi target.
	rec := Migration{
		DatabaseID: databaseID, Version: version, SQLText: sqlText,
		ExecutedBy: userID, Status: StatusSuccess,
	}
	if applyErr != nil {
		rec.Status = StatusFailed
		rec.ErrorMessage = describeError(applyErr)
	}
	// Masukun ke tabel migration_history
	err = s.platform.QueryRowContext(opCtx, `
		INSERT INTO migration_history
			(database_id, version, sql_text, executed_by, status, error_message)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, executed_at`,
		rec.DatabaseID, rec.Version, rec.SQLText, rec.ExecutedBy, rec.Status,
		sql.NullString{String: rec.ErrorMessage, Valid: rec.ErrorMessage != ""},
	).Scan(&rec.ID, &rec.ExecutedAt)
	if err != nil {
		// Perubahan mungkin sudah ter-commit di target, tapi riwayat gagal tersimpan
		return rec, fmt.Errorf("%w: %v", ErrHistoryNotRecorded, err)
	}
	return rec, nil
}

// Explain returns PostgreSQL's estimated plan without executing the submitted SQL.
// In particular, EXPLAIN ANALYZE is deliberately not used here because migration
// statements can modify target data.
func (s *Service) Explain(ctx context.Context, userID, databaseID int64, sqlText string) (string, error) {
	if userID <= 0 || databaseID <= 0 {
		return "", fmt.Errorf("%w: user and database ID must be positive", ErrInvalidArgument)
	}
	ok, err := s.authz.CanEdit(ctx, userID, databaseID)
	if err != nil {
		return "", fmt.Errorf("check edit access: %w", err)
	}
	if !ok {
		return "", ErrForbidden
	}

	sqlText = strings.TrimSpace(sqlText)
	switch {
	case sqlText == "":
		return "", ErrEmptySQL
	case len(sqlText) > maxSQLBytes:
		return "", ErrSQLTooLarge
	case strings.ContainsRune(sqlText, 0):
		return "", ErrInvalidSQL
	case strings.ContainsRune(sqlText, ';'):
		// Reject separators conservatively so a second command cannot follow
		// the EXPLAIN request through a driver/protocol mode that permits batches.
		return "", ErrExplainSingleStatement
	}

	target, err := s.targets.Get(databaseID)
	if err != nil {
		return "", err
	}
	// EXPLAIN only asks the PostgreSQL planner for a plan. A short deadline
	// bounds unusually expensive planning while never applying the statement.
	opCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var plan []byte
	if err := target.QueryRowContext(opCtx, "EXPLAIN (FORMAT JSON) "+sqlText).Scan(&plan); err != nil {
		return "", fmt.Errorf("explain migration SQL: %s", describeError(err))
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, plan, "", "  "); err != nil {
		return string(plan), nil
	}
	return pretty.String(), nil
}

// Menjalankan SQL migration dalam transaksi: commit jika semua berhasil,
// rollback jika ada yang gagal. tx selalu ditutup saat fungsi ini selesai.
func applyInTx(ctx context.Context, tx *sql.Tx, sqlText string) error {
	// SET LOCAL hanya berlaku di transaksi ini.
	for _, stmt := range []string{
		"SET LOCAL statement_timeout = '30s'",
		"SET LOCAL lock_timeout = '5s'",
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Service) lockFor(databaseID int64) *sync.Mutex {
	m, _ := s.locks.LoadOrStore(databaseID, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// describeError menyimpan pesan PostgreSQL saja, bukan err.Error() mentah,
// supaya host/user/DSN tidak ikut tersimpan di riwayat. Detail sengaja tidak
// disimpan karena bisa memuat nilai data dari tabel target.
func describeError(err error) string {
	var msg string
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		msg = "migration timed out"
	case errors.As(err, &pgErr) && pgErr.Code == "57014":
		msg = "statement timeout: migration took too long or was canceled"
	case errors.As(err, &pgErr):
		msg = pgErr.Message
	default:
		msg = "migration failed"
	}
	if len(msg) > maxErrorMessageLen {
		msg = strings.ToValidUTF8(msg[:maxErrorMessageLen], "")
	}
	return msg
}
