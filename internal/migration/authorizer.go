package migration

import (
	"context"
	"fmt"
	"github.com/kelompok/if3110-tubes/internal/access"
)

// AccessAuthorizer menyambungkan Authorizer ke modul akses P1
type AccessAuthorizer struct {
	Access access.Service
}

var _ Authorizer = AccessAuthorizer{}

func (a AccessAuthorizer) level(ctx context.Context, userID, databaseID int64) (access.Level, error) {
	lvl, err := a.Access.EffectiveAccess(ctx, userID, databaseID)
	if err != nil {
		return access.NoAccess, fmt.Errorf("effective access: %w", err)
	}
	return lvl, nil
}

func (a AccessAuthorizer) CanView(ctx context.Context, userID, databaseID int64) (bool, error) {
	lvl, err := a.level(ctx, userID, databaseID)
	return lvl >= access.ViewOnly, err
}

func (a AccessAuthorizer) CanEdit(ctx context.Context, userID, databaseID int64) (bool, error) {
	lvl, err := a.level(ctx, userID, databaseID)
	return lvl >= access.ViewEdit, err
}