package qdrant_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jpgomesr/NeuralVault/internal/config"
	localqdrant "github.com/jpgomesr/NeuralVault/internal/vectorstorage/qdrant"
)

var sharedCfg *config.Config

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "qdrant/qdrant:v1.18.2",
			ExposedPorts: []string{"6333/tcp", "6334/tcp"},
			WaitingFor:   wait.ForHTTP("/healthz").WithPort("6333/tcp"),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start qdrant container: %v\n", err)
		return 1
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	host, err := ctr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get container host: %v\n", err)
		return 1
	}
	grpcPort, err := ctr.MappedPort(ctx, "6334")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get mapped grpc port: %v\n", err)
		return 1
	}

	sharedCfg = &config.Config{
		Qdrant: config.Qdrant{
			URL:            host,
			GrpcPort:       int(grpcPort.Num()),
			APIKey:         "",
			UseTLS:         false,
			CollectionName: "test",
			VectorSize:     768,
		},
	}

	return m.Run()
}

// unreachableCfg returns a config pointing at a port with nothing listening.
func unreachableCfg() *config.Config {
	return &config.Config{
		Qdrant: config.Qdrant{
			URL:            "localhost",
			GrpcPort:       59998,
			APIKey:         "",
			UseTLS:         false,
			CollectionName: "test",
			VectorSize:     768,
		},
	}
}

// TestNewPool_Success verifies a client connects and HealthCheck succeeds.
func TestNewPool_Success(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.Qdrant{
			URL:            sharedCfg.Qdrant.URL,
			GrpcPort:       sharedCfg.Qdrant.GrpcPort,
			APIKey:         sharedCfg.Qdrant.APIKey,
			UseTLS:         sharedCfg.Qdrant.UseTLS,
			CollectionName: "test_newpool_success",
			VectorSize:     768,
		},
	}

	client, err := localqdrant.NewPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	t.Cleanup(func() {
		_ = client.DeleteCollection(context.Background(), cfg.Qdrant.CollectionName)
		_ = client.Close()
	})
}

// TestNewPool_EnsureCollectionAlreadyExists covers the "collection exists" branch.
func TestNewPool_EnsureCollectionAlreadyExists(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.Qdrant{
			URL:            sharedCfg.Qdrant.URL,
			GrpcPort:       sharedCfg.Qdrant.GrpcPort,
			APIKey:         sharedCfg.Qdrant.APIKey,
			UseTLS:         sharedCfg.Qdrant.UseTLS,
			CollectionName: "test_idempotent_collection",
			VectorSize:     768,
		},
	}

	client1, err := localqdrant.NewPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first call: expected no error, got: %v", err)
	}
	t.Cleanup(func() {
		_ = client1.DeleteCollection(context.Background(), cfg.Qdrant.CollectionName)
		_ = client1.Close()
	})

	client2, err := localqdrant.NewPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second call: expected no error, got: %v", err)
	}
	_ = client2.Close()
}

// TestNewPool_Failure verifies an error is returned when Qdrant is unreachable.
func TestNewPool_Failure(t *testing.T) {
	_, err := localqdrant.NewPool(context.Background(), unreachableCfg())
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
}
