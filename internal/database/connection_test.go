
package database

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestOpenTarget(t *testing.T) {
	dsn := os.Getenv("TARGET_DATABASE_URL")
	if dsn == "" {
		t.Skip("TARGET_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	db, err := OpenTarget(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	var result int
	err = db.QueryRowContext(ctx, "SELECT 1").Scan(&result)
	if err != nil {
		t.Fatalf("failed to execute test query: %v", err)
	}

	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}
