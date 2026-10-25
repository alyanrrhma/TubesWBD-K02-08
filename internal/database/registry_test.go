package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func registryTestDSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("TARGET_DATABASE_URL")
	if dsn == "" {
		t.Skip("TARGET_DATABASE_URL not set")
	}

	return dsn
}

func TestRegistryConnectAndGet(t *testing.T) {
	dsn := registryTestDSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	registry := NewRegistry()
	t.Cleanup(func() {
		if err := registry.CloseAll(); err != nil {
			t.Errorf("CloseAll failed: %v", err)
		}
	})

	const databaseID int64 = 1

	if err := registry.Connect(ctx, databaseID, dsn); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	db, err := registry.Get(databaseID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	var result int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		t.Fatalf("query through registered connection failed: %v", err)
	}

	if result != 1 {
		t.Errorf("expected result 1, got %d", result)
	}
}

func TestRegistryRejectsDuplicateID(t *testing.T) {
	dsn := registryTestDSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	registry := NewRegistry()
	t.Cleanup(func() {
		if err := registry.CloseAll(); err != nil {
			t.Errorf("CloseAll failed: %v", err)
		}
	})

	const databaseID int64 = 1

	if err := registry.Connect(ctx, databaseID, dsn); err != nil {
		t.Fatalf("first Connect failed: %v", err)
	}

	// ID yang sama tidak boleh didaftarkan dua kali.
	if err := registry.Connect(ctx, databaseID, dsn); err == nil {
		t.Fatal("expected error when registering duplicate database ID")
	}

	// Koneksi pertama harus tetap dapat digunakan.
	db, err := registry.Get(databaseID)
	if err != nil {
		t.Fatalf("Get after duplicate registration failed: %v", err)
	}

	var result int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		t.Fatalf("existing connection should remain usable: %v", err)
	}
}

func TestRegistryGetUnregisteredID(t *testing.T) {
	registry := NewRegistry()

	_, err := registry.Get(999)
	if !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("expected ErrNotRegistered, got %v", err)
	}
}

func TestRegistryClose(t *testing.T) {
	dsn := registryTestDSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	registry := NewRegistry()

	const databaseID int64 = 1

	if err := registry.Connect(ctx, databaseID, dsn); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if err := registry.Close(databaseID); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Setelah ditutup, ID tidak lagi tersedia di registry.
	if _, err := registry.Get(databaseID); err == nil {
		t.Fatal("expected error after closing the registered connection")
	}

	// Menutup ID yang sudah tidak terdaftar harus menghasilkan error.
	if err := registry.Close(databaseID); err == nil {
		t.Fatal("expected error when closing an unregistered ID")
	}
}

func TestRegistryCloseAll(t *testing.T) {
	dsn := registryTestDSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	registry := NewRegistry()

	if err := registry.Connect(ctx, 1, dsn); err != nil {
		t.Fatalf("Connect database 1 failed: %v", err)
	}

	if err := registry.Connect(ctx, 2, dsn); err != nil {
		t.Fatalf("Connect database 2 failed: %v", err)
	}

	if err := registry.CloseAll(); err != nil {
		t.Fatalf("CloseAll failed: %v", err)
	}

	for _, id := range []int64{1, 2} {
		if _, err := registry.Get(id); err == nil {
			t.Errorf("expected database %d to be removed after CloseAll", id)
		}
	}

	// Registry yang sudah kosong tetap bisa ditutup dengan aman.
	if err := registry.CloseAll(); err != nil {
		t.Errorf("second CloseAll failed: %v", err)
	}
}

func TestRegistryRejectsInvalidIDAndDSN(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()

	if err := registry.Connect(ctx, 0, "some-dsn"); err == nil {
		t.Error("expected error for database ID 0")
	}
	if err := registry.Connect(ctx, -1, "some-dsn"); err == nil {
		t.Error("expected error for negative database ID")
	}

	if err := registry.Connect(ctx, 1, ""); err == nil {
		t.Error("expected error for empty DSN")
	}

	if _, err := registry.Get(0); err == nil {
		t.Error("expected error when getting database ID 0")
	}

	if err := registry.Close(0); err == nil {
		t.Error("expected error when closing database ID 0")
	}
}

func TestRegistryErrors(t *testing.T) {
	registry := NewRegistry()

	if _, err := registry.Get(1); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("Get unknown: got %v", err)
	}
	if _, err := registry.Get(0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("Get 0: got %v", err)
	}
	if err := registry.Close(1); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("Close unknown: got %v", err)
	}
	if err := registry.Connect(context.Background(), 1, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("Connect empty DSN: got %v", err)
	}
}