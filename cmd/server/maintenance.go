package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/platformdb"
)

// purgeSessions membersihkan session kadaluarsa secara berkala.
func purgeSessions(ctx context.Context, log *slog.Logger, db *sql.DB) {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := platformdb.PurgeExpiredSessions(ctx, db)
			if err != nil {
				log.Error("purge session gagal", "err", err)
			} else if n > 0 {
				log.Info("session kadaluarsa dibersihkan", "baris", n)
			}
		}
	}
}