package migration

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

type Targets interface {
	Get(databaseID int64) (*sql.DB, error)
}

type Authorizer interface {
	CanView(ctx context.Context, userID, databaseID int64) (bool, error)
	CanEdit(ctx context.Context, userID, databaseID int64) (bool, error)
}

type Status string

const (
	StatusSuccess Status = "SUCCESS"
	StatusFailed  Status = "FAILED"
)

type Migration struct {
	ID             int64     `json:"id"`
	DatabaseID     int64     `json:"database_id"`
	Version        int64     `json:"version"`
	SQLText        string    `json:"sql_text,omitempty"`
	ExecutedBy     int64     `json:"executed_by"`
	ExecutedByName string    `json:"executed_by_name"`
	ExecutedAt     time.Time `json:"executed_at"`
	Status         Status    `json:"status"`
	ErrorMessage   string    `json:"error_message,omitempty"`
}

type Service struct {
	platform *sql.DB
	targets  Targets
	authz    Authorizer
	locks    sync.Map // databaseID -> *sync.Mutex
}