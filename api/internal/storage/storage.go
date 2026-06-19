package storage

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/storage/postgres"
)

// Pool is the interface that wraps the relational database connection pool.
// Concrete implementations must satisfy this interface.
type Pool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
	Ping(ctx context.Context) error
	Close()
}

// NewPool creates and returns a Pool backed by PostgreSQL.
func NewPool(ctx context.Context, cfg config.Config) (Pool, error) {
	return postgres.NewPool(ctx, cfg)
}
