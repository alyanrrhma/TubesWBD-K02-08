
package database

import (
	"context"
	"testing"
	"time"
)

func TestOpenTargetRejectsEmptyDSN(t *testing.T) {
	_, err := OpenTarget(context.Background(), "")
	if err == nil {
		t.Fatal("expected an error for empty DSN")
	}
}

func TestOpenTargetRejectsUnreachableDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		2*time.Second,
	)
	defer cancel()

	// Port 1 digunakan sebagai alamat uji yang biasanya tidak
	// menjalankan PostgreSQL. Tidak memerlukan database tambahan.
	dsn := "postgres://dbmp:wrong@127.0.0.1:1/dbmp_target?sslmode=disable"

	db, err := OpenTarget(ctx, dsn)
	if err == nil {
		if db != nil {
			db.Close()
		}
		t.Fatal("expected an error for unreachable database")
	}
}
