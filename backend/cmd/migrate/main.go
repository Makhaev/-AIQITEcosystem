package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	core "aiq.local/core"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/logging"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		logging.Failure("database migration failed", err)
		os.Exit(1)
	}
	slog.Info("database migrations applied")
}

func run() error {
	connection := os.Getenv("DATABASE_URL")
	if connection == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, connection)
	if err != nil {
		return err
	}
	defer pool.Close()
	return database.Migrate(ctx, pool, core.Migrations)
}
