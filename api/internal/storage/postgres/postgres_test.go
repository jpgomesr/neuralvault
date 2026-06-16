package postgres_test

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/storage/postgres"
)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envPortOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			return p
		}
	}
	return def
}

// integrationConfig builds a config from environment variables and skips
// the test when POSTGRES_HOST is not set (no live database available).
func integrationConfig(t *testing.T) config.Config {
	t.Helper()
	if os.Getenv("POSTGRES_HOST") == "" {
		t.Skip("POSTGRES_HOST not set; skipping postgres integration test")
	}
	return config.Config{
		Postgres: config.Postgres{
			Host:     envOrDefault("POSTGRES_HOST", "localhost"),
			Port:     envPortOrDefault("POSTGRES_PORT", 5432),
			Username: envOrDefault("POSTGRES_USERNAME", "neuralvault"),
			Password: envOrDefault("POSTGRES_PASSWORD", "neuralvault"),
			Name:     envOrDefault("POSTGRES_NAME", "neuralvault"),
			SSLMode:  "disable",
			MaxConns: 5,
			MinConns: 1,
		},
	}
}

// unreachableConfig returns a config pointing to a port that has nothing
// listening on it, so Ping will fail quickly with "connection refused".
func unreachableConfig() config.Config {
	return config.Config{
		Postgres: config.Postgres{
			Host:     "localhost",
			Port:     59998,
			Username: "user",
			Password: "pass",
			Name:     "db",
			SSLMode:  "disable",
			MaxConns: 2,
			MinConns: 1,
		},
	}
}

// TestNewPool_Success verifies a pool is returned and Ping succeeds against
// a live database. Requires POSTGRES_HOST to be set in the environment.
func TestNewPool_Success(t *testing.T) {
	cfg := integrationConfig(t)

	pool, err := postgres.NewPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer pool.Close()

	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
}

// TestNewPool_ParseConfigFailure verifies that NewPool returns an error
// when the DSN is malformed (unquoted space in host breaks the key=value parser).
func TestNewPool_ParseConfigFailure(t *testing.T) {
	cfg := config.Config{
		Postgres: config.Postgres{
			Host:     "local host", // unquoted space splits into two tokens; pgx rejects it
			Port:     5432,
			Username: "user",
			Password: "pass",
			Name:     "db",
			SSLMode:  "disable",
			MaxConns: 2,
			MinConns: 0,
		},
	}

	_, err := postgres.NewPool(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for malformed DSN, got nil")
	}
}

// TestNewPool_PingFailure verifies that NewPool returns an error when the
// database is unreachable (connection refused at Ping time).
func TestNewPool_PingFailure(t *testing.T) {
	cfg := unreachableConfig()

	_, err := postgres.NewPool(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
}

// TestNewPool_CancelledContext verifies that a pre-cancelled context causes
// NewPool to return an error rather than blocking.
func TestNewPool_CancelledContext(t *testing.T) {
	cfg := integrationConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := postgres.NewPool(ctx, cfg)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
