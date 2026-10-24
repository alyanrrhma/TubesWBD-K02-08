package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreCompareAndSwapSuccess(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	oldHash := digest("original-session-token")
	newHash := digest("rotated-session-token")

	session := Session{
		FamilyID:  "test-family-1",
		Principal: Principal{UserID: 1},
		TokenHash: oldHash,
		CSRFHash:  digest("csrf-token"),
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		RotatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}

	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Simulate a successful token rotation.
	updated := session
	updated.TokenHash = newHash

	if err := store.CompareAndSwap(ctx, updated, oldHash); err != nil {
		t.Fatalf("CompareAndSwap() error = %v", err)
	}

	// A stale update using the old hash must be rejected.
	stale := session
	stale.LastSeen = time.Now().Add(time.Minute)

	err := store.CompareAndSwap(ctx, stale, oldHash)
	if !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("expected ErrSessionConflict, got %v", err)
	}

	// Verify that the successful rotation was not overwritten.
	got, err := store.FindByTokenHash(ctx, newHash)
	if err != nil {
		t.Fatalf("FindByTokenHash() error = %v", err)
	}
	if got.TokenHash != newHash {
		t.Fatal("session token hash was unexpectedly overwritten")
	}
}

func TestMemoryStoreCompareAndSwapRejectsRevokedSession(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	tokenHash := digest("session-token-to-revoke")
	session := Session{
		FamilyID:  "test-family-2",
		Principal: Principal{UserID: 2},
		TokenHash: tokenHash,
		CSRFHash:  digest("csrf-token"),
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		RotatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}

	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := store.RevokeFamily(ctx, session.FamilyID); err != nil {
		t.Fatalf("RevokeFamily() error = %v", err)
	}

	updated := session
	updated.LastSeen = time.Now().Add(time.Minute)

	err := store.CompareAndSwap(ctx, updated, tokenHash)
	if !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("expected ErrSessionConflict for revoked session, got %v", err)
	}
}