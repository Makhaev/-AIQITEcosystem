package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"aiq.local/core/internal/backup"
)

func main() { os.Exit(run()) }
func run() int {
	c := backup.Config{URL: os.Getenv("DATABASE_URL"), Store: os.Getenv("BACKUP_STORE"), State: os.Getenv("BACKUP_STATE"), BinDir: os.Getenv("PG_BIN"), Hour: 3}
	if len(os.Args) < 2 || c.Store == "" || c.State == "" {
		fmt.Fprintln(os.Stderr, "usage: backup run|serve|status|restore ID; set BACKUP_STORE, BACKUP_STATE, DATABASE_URL")
		return 2
	}
	if value := os.Getenv("BACKUP_TIME"); value != "" {
		parts := strings.Split(value, ":")
		if len(parts) != 2 {
			return 2
		}
		var err error
		c.Hour, err = strconv.Atoi(parts[0])
		if err != nil {
			return 2
		}
		c.Minute, err = strconv.Atoi(parts[1])
		if err != nil || c.Hour < 0 || c.Hour > 23 || c.Minute < 0 || c.Minute > 59 {
			return 2
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	output := json.NewEncoder(os.Stdout)
	switch os.Args[1] {
	case "status":
		s, _ := c.ReadStatus()
		_ = output.Encode(s)
		if c.Health(time.Now()) != nil {
			return 1
		}
		return 0
	case "run":
		job, stop := context.WithTimeout(ctx, 2*time.Hour)
		defer stop()
		_, err := c.Run(job, "")
		s, _ := c.ReadStatus()
		_ = output.Encode(s)
		if err != nil {
			fmt.Fprintln(os.Stderr, "backup failed; inspect status stage and operator runbook")
			return 1
		}
		return 0
	case "restore":
		if len(os.Args) != 3 {
			return 2
		}
		job, stop := context.WithTimeout(ctx, 2*time.Hour)
		defer stop()
		r, err := c.Restore(job, os.Args[2])
		_ = output.Encode(r)
		if err != nil {
			fmt.Fprintln(os.Stderr, "restore failed; retain report and isolated database for diagnosis")
			return 1
		}
		return 0
	case "serve":
		c.Serve(ctx, func(s backup.Status, err error) {
			_ = output.Encode(s)
			if err != nil {
				fmt.Fprintln(os.Stderr, "scheduled backup failed")
			}
		})
		return 0
	default:
		return 2
	}
}
