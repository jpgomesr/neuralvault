package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/storage/postgres"
	"github.com/pressly/goose/v3"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		_, err := fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		if err != nil {
			return
		}
		os.Exit(1)
	}

	pool, err := postgres.NewPool(ctx, *cfg)
	if err != nil {
		_, err := fmt.Fprintf(os.Stderr, "Error connecting to database: %v\n", err)
		if err != nil {
			return
		}
		os.Exit(1)
	}
	defer pool.Close()

	db := stdlib.OpenDBFromPool(pool)
	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			_, err := fmt.Fprintf(os.Stderr, "Error closing db: %v\n", err)
			if err != nil {
				return
			}
			os.Exit(1)
		}
	}(db)

	if err := goose.SetDialect("postgres"); err != nil {
		fmt.Fprintf(os.Stderr, "setting dialect: %v\n", err)
		os.Exit(1)
	}

	// get commands: up, down...
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: migrate <command>")
		os.Exit(1)
	}

	command := os.Args[1]
	if err := goose.RunContext(ctx, command, db, "internal/storage/postgres/migrations"); err != nil {
		fmt.Fprintf(os.Stderr, "goose %s: %v\n", command, err)
		os.Exit(1)
	}
}
