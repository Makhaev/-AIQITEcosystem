package main

import (
	"context"
	"fmt"
	"os"
	"time"

	core "aiq.local/core"
	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/database"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	in := users.CreateInput{Email: os.Getenv("ADMIN_EMAIL"), Name: os.Getenv("ADMIN_NAME"), Password: os.Getenv("ADMIN_PASSWORD")}
	// Remove the secret from this process environment as soon as it is read.
	_ = os.Unsetenv("ADMIN_PASSWORD")
	if in.Email == "" || in.Name == "" || in.Password == "" {
		return fmt.Errorf("ADMIN_EMAIL, ADMIN_NAME and ADMIN_PASSWORD are required; no default password")
	}
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
	if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
		return err
	}
	service := users.Service{Repo: &users.Repository{Pool: pool}}
	u, err := service.Bootstrap(ctx, in)
	if err != nil {
		return err
	}
	fmt.Printf("Created initial administrator (id=%d).\n", u.ID)
	return nil
}
