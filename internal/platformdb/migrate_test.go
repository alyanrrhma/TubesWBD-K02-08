package platformdb

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMigrateIsIdempotent(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for i := 0; i < 2; i++ { // kedua kali harus berhasil tanpa efek samping
		if err := Migrate(ctx, db); err != nil {
			t.Fatalf("Migrate #%d: %v", i+1, err)
		}
	}
	var n int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'database_connections' AND column_name IN ('host','port','database_name','username','ssl_mode','password_encrypted')`).Scan(&n)
	if err != nil || n != 6 {
		t.Fatalf("kolom detail koneksi tidak lengkap: n=%d err=%v", n, err)
	}
	if _, err := PurgeExpiredSessions(ctx, db); err != nil {
		t.Fatalf("PurgeExpiredSessions: %v", err)
	}
}