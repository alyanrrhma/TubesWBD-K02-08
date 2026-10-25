//Koneksi ke database target yang ingin dikelola
// di script/ adalah contoh database yang dibuat

package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenTarget membuka dan memeriksa koneksi ke database target.
const connectTimeout = 5 * time.Second

func OpenTarget(ctx context.Context, dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("%w: target database DSN is empty", ErrInvalidArgument)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: open target database", ErrConnectFailed)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: ping target database", ErrConnectFailed)
	}
	return db, nil
}