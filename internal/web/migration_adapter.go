package web

import (
	"context"
	"errors"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/migration"
)

// MigrationAdapter adapts the migration module to the HTTP layer contract.
type MigrationAdapter struct {
	Service *migration.Service
}

var _ Migrations = MigrationAdapter{}

func (a MigrationAdapter) History(ctx context.Context, userID, databaseID int64, page, pageSize int) ([]MigrationRecord, int, error) {
	if a.Service == nil {
		return nil, 0, errors.New("migration service is not configured")
	}
	items, total, err := a.Service.ListHistory(ctx, userID, databaseID, page, pageSize)
	if errors.Is(err, migration.ErrForbidden) {
		return nil, 0, access.ErrForbidden
	}
	if err != nil {
		return nil, 0, err
	}
	return migrationRecords(items), total, nil
}

func (a MigrationAdapter) Detail(ctx context.Context, userID, databaseID, version int64) (*MigrationRecord, error) {
	if a.Service == nil {
		return nil, errors.New("migration service is not configured")
	}

	item, err := a.Service.GetMigration(ctx, userID, databaseID, version)
	if errors.Is(err, migration.ErrForbidden) {
		return nil, access.ErrForbidden
	}
	if errors.Is(err, migration.ErrMigrationNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	record := migrationRecords([]migration.Migration{item})[0]
	return &record, nil
}

func (a MigrationAdapter) Execute(ctx context.Context, userID, databaseID int64, sqlText string) (*MigrationRecord, error) {
	if a.Service == nil {
		return nil, errors.New("migration service is not configured")
	}
	item, err := a.Service.Execute(ctx, userID, databaseID, sqlText)
	if errors.Is(err, migration.ErrForbidden) {
		return nil, access.ErrForbidden
	}
	if err != nil {
		return nil, err
	}
	record := migrationRecords([]migration.Migration{item})[0]
	return &record, nil
}

func (a MigrationAdapter) Explain(ctx context.Context, userID, databaseID int64, sqlText string) (string, error) {
	if a.Service == nil {
		return "", errors.New("migration service is not configured")
	}
	plan, err := a.Service.Explain(ctx, userID, databaseID, sqlText)
	if errors.Is(err, migration.ErrForbidden) {
		return "", access.ErrForbidden
	}
	return plan, err
}

func migrationRecords(items []migration.Migration) []MigrationRecord {
	records := make([]MigrationRecord, 0, len(items))
	for _, item := range items {
		records = append(records, MigrationRecord{
			ID:           item.ID,
			Version:      int(item.Version),
			SQL:          item.SQLText,
			ExecutedBy:   item.ExecutedByName,
			ExecutedAt:   item.ExecutedAt,
			Status:       string(item.Status),
			ErrorMessage: item.ErrorMessage,
		})
	}
	return records
}
