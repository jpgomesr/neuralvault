package postgres_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/storage/postgres"
)

var sharedCfg config.Config

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:17",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     "neuralvault",
				"POSTGRES_PASSWORD": "neuralvault",
				"POSTGRES_DB":       "neuralvault",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres container: %v\n", err)
		return 1
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	host, err := ctr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get container host: %v\n", err)
		return 1
	}
	port, err := ctr.MappedPort(ctx, "5432")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get mapped port: %v\n", err)
		return 1
	}

	sharedCfg = config.Config{
		Postgres: config.Postgres{
			Host:     host,
			Port:     int(port.Num()),
			Username: "neuralvault",
			Password: "neuralvault",
			Name:     "neuralvault",
			SSLMode:  "disable",
			MaxConns: 5,
			MinConns: 1,
		},
	}

	return m.Run()
}

// unreachableConfig returns a config pointing at a port with nothing listening.
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

func TestNewPool_Success(t *testing.T) {
	pool, err := postgres.NewPool(context.Background(), sharedCfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer pool.Close()

	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
}

// TestNewPool_ParseConfigFailure verifies that a malformed DSN is rejected
// before any network connection is attempted.
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

func TestNewPool_PingFailure(t *testing.T) {
	_, err := postgres.NewPool(context.Background(), unreachableConfig())
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
}

func TestNewPool_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := postgres.NewPool(ctx, sharedCfg)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
