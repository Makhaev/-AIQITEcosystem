package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	core "aiq.local/core"
	"aiq.local/core/internal/app"
	"aiq.local/core/internal/platform/config"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/logging"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		logging.Failure("startup failed", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := database.Open(startup, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = database.Migrate(startup, pool, core.Migrations); err != nil {
		return err
	}
	router, repo, err := app.Router(pool, cfg)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.Address, Handler: router, ErrorLog: log.New(logging.HTTPErrorWriter{}, "", 0), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { slog.Info("server listening"); done <- server.ListenAndServe() }()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ticker.C:
			cleanup, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := repo.Cleanup(cleanup)
			cancel()
			if err != nil {
				slog.Error("session cleanup failed")
			}
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return server.Shutdown(shutdown)
		}
	}
}
