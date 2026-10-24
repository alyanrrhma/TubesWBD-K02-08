// Untuk kelola sejumlah koneksi aktif ke target
package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

type Registry struct {
	mu          sync.RWMutex
	connections map[int64]*sql.DB
}

// NewRegistry membuat registry koneksi yang kosong.
func NewRegistry() *Registry {
	return &Registry{
		connections: make(map[int64]*sql.DB),
	}
}

// Connect membuka koneksi ke database target dan mendaftarkannya berdasarkan ID database yang unik.
func (r *Registry) Connect(ctx context.Context, databaseID int64, dsn string) error {
	if r == nil {
		return fmt.Errorf("database registry is nil")
	}
	if databaseID <= 0 {
		return fmt.Errorf("%w: database ID must be positive", ErrInvalidArgument)
	}
	if dsn == "" {
		return fmt.Errorf("%w: target database DSN is empty", ErrInvalidArgument)
	}

	r.mu.RLock()
	_, exists := r.connections[databaseID]
	r.mu.RUnlock()
	if exists {
		return fmt.Errorf("%w: id %d", ErrAlreadyRegistered, databaseID)
	}

	db, err := OpenTarget(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to target database %d: %w", databaseID, err)
	}

	// Periksa kembali di bawah write lock untuk menangani
	// kemungkinan dua pemanggilan Connect secara bersamaan.
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.connections[databaseID]; exists {
		db.Close()
		return fmt.Errorf("%w: id %d", ErrAlreadyRegistered, databaseID)
	}

	r.connections[databaseID] = db
	return nil
}

// Get mengambil koneksi aktif berdasarkan ID database.
func (r *Registry) Get(databaseID int64) (*sql.DB, error) {
	if r == nil {
		return nil, fmt.Errorf("database registry is nil")
	}
	if databaseID <= 0 {
		return nil, fmt.Errorf("%w: database ID must be positive", ErrInvalidArgument)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	db, exists := r.connections[databaseID]
	if !exists {
		return nil, fmt.Errorf("%w: id %d", ErrNotRegistered, databaseID)
	}

	return db, nil
}

// Close menghapus dan menutup koneksi aktif berdasarkan ID database.
func (r *Registry) Close(databaseID int64) error {
	if r == nil {
		return fmt.Errorf("database registry is nil")
	}
	if databaseID <= 0 {
		return fmt.Errorf("%w: database ID must be positive", ErrInvalidArgument)
	}

	r.mu.Lock()
	db, exists := r.connections[databaseID]
	if exists {
		delete(r.connections, databaseID)
	}
	r.mu.Unlock()

	if !exists {
		return fmt.Errorf("%w: id %d", ErrNotRegistered, databaseID)
	}

	if err := db.Close(); err != nil {
		return fmt.Errorf(
			"close database connection %d: %w",
			databaseID,
			err,
		)
	}

	return nil
}

// CloseAll menghapus dan menutup seluruh koneksi yang terdaftar.
// Semua koneksi dihapus dari registry meskipun ada yang gagal ditutup.
func (r *Registry) CloseAll() error {
	if r == nil {
		return fmt.Errorf("database registry is nil")
	}

	r.mu.Lock()
	connections := r.connections
	r.connections = make(map[int64]*sql.DB)
	r.mu.Unlock()

	var closeErrors []error

	for id, db := range connections {
		if err := db.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf(
				"close database connection %d: %w",
				id,
				err,
			))
		}
	}

	if len(closeErrors) > 0 {
		return fmt.Errorf(
			"failed to close %d database connection(s): %v",
			len(closeErrors),
			closeErrors,
		)
	}

	return nil
}