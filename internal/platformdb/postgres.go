// Koneksi ke database internal
// untuk kebutuhan internal aplikasi DBMP, seperti menyimpan akun, sesi, konfigurasi koneksi database pengguna, dan riwayat migrasi
package platformdb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"  //pgx driver untuk PostgreSQL
)

// Open membuka dan memeriksa koneksi ke database internal
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("platform database DSN is empty")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open platform database: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping platform database: %w", err)
	}

	return db, nil
}
