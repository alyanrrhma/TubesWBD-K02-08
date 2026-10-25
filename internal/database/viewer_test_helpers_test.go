// Helper tabel dan skema sementara untuk testing
package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

func openTestDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()

	dsn := os.Getenv("TARGET_DATABASE_URL")
	if dsn == "" {
		t.Skip("TARGET_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)

	db, err := OpenTarget(ctx, dsn)
	if err != nil {
		cancel()
		t.Fatalf("failed to connect: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
		cancel()
	})

	return db, ctx
}

// Membuat schema dan tabel sementara khusus untuk testing.
func createViewerTestTable(t *testing.T) (*sql.DB, context.Context, string) {
	t.Helper()

	db, ctx := openTestDB(t)

	schema := fmt.Sprintf("viewer_test_%d", time.Now().UnixNano())
	quotedSchema := quoteIdentifier(schema)

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(), 5*time.Second,
		)
		defer cancel()

		_, err := db.ExecContext(
			cleanupCtx,
			"DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE",
		)
		if err != nil {
			t.Errorf("failed to drop test schema: %v", err)
		}
	})

	table := quotedSchema + "." + quoteIdentifier("items")

	_, err := db.ExecContext(ctx, `
		CREATE TABLE `+table+` (
			id INTEGER PRIMARY KEY,
			nama TEXT NOT NULL,
			jurusan TEXT
		)
	`)
	if err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO `+table+` (id, nama, jurusan)
		VALUES
			(1, 'Andi', 'Informatika'),
			(2, 'Budi', 'Sistem Informasi')
	`)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	return db, ctx, schema
}