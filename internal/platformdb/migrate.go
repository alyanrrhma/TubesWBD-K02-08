package platformdb

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql migrations/*.sql
var schemaFS embed.FS

const migrateLockID = 31100001 // mencegah dua instance memigrasi bersamaan

// Migrate menerapkan schema.sql dan migration yang belum tercatat
func Migrate(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("platform migrate: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrateLockID); err != nil {
		return fmt.Errorf("platform migrate: lock: %w", err)
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrateLockID)

	base, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return err
	}
	if err := execScript(ctx, conn, string(base)); err != nil {
		return fmt.Errorf("platform migrate: schema.sql: %w", err)
	}

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS platform_schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(schemaFS, "migrations")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM platform_schema_migrations WHERE name = $1)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := schemaFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		// Satu string multi-statement dijalankan sebagai satu transaksi implisit,
		// sehingga pencatatan nama ikut batal bila salah satu statement gagal.
		script := string(body) + "\n;INSERT INTO platform_schema_migrations (name) VALUES ('" + strings.ReplaceAll(name, "'", "''") + "');"
		if err := execScript(ctx, conn, script); err != nil {
			return fmt.Errorf("platform migrate: %s: %w", name, err)
		}
	}
	return nil
}

// execScript menjalankan SQL multi-statement lewat simple protocol
func execScript(ctx context.Context, conn *sql.Conn, script string) error {
	return conn.Raw(func(driverConn any) error {
		c, ok := driverConn.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("driver bukan pgx stdlib")
		}
		_, err := c.Conn().PgConn().Exec(ctx, script).ReadAll()
		return err
	})
}

// PurgeExpiredSessions menghapus baris session yang sudah tidak mungkin dipakai lagi (kadaluarsa absolut atau dicabut lama) agar tabel sessions tidak membengkak
func PurgeExpiredSessions(ctx context.Context, db *sql.DB) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM sessions
		WHERE expires_at < NOW() - INTERVAL '1 hour'
		   OR (revoked AND last_seen < NOW() - INTERVAL '1 day')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}