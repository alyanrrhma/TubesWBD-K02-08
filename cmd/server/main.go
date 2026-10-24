// Command server menjalankan Database Management Platform.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/admin"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/config"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/migration"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/platformdb"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/web"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("aplikasi berhenti", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return errors.New("DATABASE_URL wajib diisi (lihat .env.example)")
	}
	if cfg.UsingDevSecretKey {
		log.Warn("APP_SECRET_KEY kosong: memakai kunci pengembangan bawaan (hanya untuk lokal)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	platformDB, err := platformdb.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer platformDB.Close()
	if err := platformdb.Migrate(ctx, platformDB); err != nil {
		return err
	}
	log.Info("platform database siap")

	sessions, err := auth.NewManager(auth.PostgresStore{DB: platformDB}, cfg.Session)
	if err != nil {
		return err
	}
	accessService := access.Service{Repo: access.PostgresRepository{DB: platformDB}}

	targets := database.NewRegistry()
	defer targets.CloseAll()
	adminService, err := admin.New(platformDB, targets, sessions, cfg.SecretKey)
	if err != nil {
		return err
	}
	if cfg.BootstrapAdminUsername != "" {
		created, err := adminService.EnsureAdmin(ctx, cfg.BootstrapAdminUsername, cfg.BootstrapAdminPassword)
		if err != nil {
			return err
		}
		if created {
			log.Info("Platform Admin pertama dibuat", "username", cfg.BootstrapAdminUsername)
		}
	}

	connected, failures, err := adminService.ConnectAll(ctx)
	if err != nil {
		return err
	}
	log.Info("koneksi database target diaktifkan", "berhasil", connected, "gagal", len(failures))
	for id, connectErr := range failures {
		log.Warn("koneksi target gagal diaktifkan", "id", id, "err", connectErr)
	}

	migrations := migration.NewService(platformDB, targets, migration.AccessAuthorizer{Access: accessService})
	server, err := web.New(web.Deps{
		Config:     cfg,
		Logger:     log,
		Sessions:   sessions,
		Users:      auth.PostgresUsers{DB: platformDB},
		Admin:      adminService,
		Viewer:     web.DatabaseViewer{Access: accessService, Conns: adminService, Registry: targets},
		Migrations: web.MigrationAdapter{Service: migrations},
		Health:     platformDB.PingContext,
	})
	if err != nil {
		return err
	}

	go purgeSessions(ctx, log, platformDB)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("server berjalan", "addr", cfg.Addr, "env", cfg.Env)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("mematikan server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
	return nil
}