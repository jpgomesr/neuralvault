package qdrant_test

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/jpgomesr/NeuralVault/internal/config"
	localqdrant "github.com/jpgomesr/NeuralVault/internal/vectorstorage/qdrant"
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

func integrationCfg(t *testing.T, collectionName string) *config.Config {
	t.Helper()
	if os.Getenv("QDRANT_URL") == "" {
		t.Skip("QDRANT_URL not set; skipping qdrant integration test")
	}
	return &config.Config{
		Qdrant: config.Qdrant{
			URL:            envOrDefault("QDRANT_URL", "localhost"),
			GrpcPort:       envPortOrDefault("QDRANT_GRPC_PORT", 6334),
			APIKey:         envOrDefault("QDRANT_API_KEY", ""),
			UseTLS:         false,
			CollectionName: collectionName,
			VectorSize:     768,
		},
	}
}

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

// TestNewPool_Success verifies a client is returned and HealthCheck succeeds
// against a live Qdrant instance. Requires QDRANT_URL to be set.
func TestNewPool_Success(t *testing.T) {
	cfg := integrationCfg(t, "test_newpool_success")

	client, err := localqdrant.NewPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	t.Cleanup(func() {
		_ = client.DeleteCollection(context.Background(), cfg.Qdrant.CollectionName)
		_ = client.Close()
	})
}

// TestNewPool_EnsureCollectionAlreadyExists verifies that calling NewPool a
// second time with the same collection name succeeds (covers the exists branch
// inside ensureCollection).
func TestNewPool_EnsureCollectionAlreadyExists(t *testing.T) {
	cfg := integrationCfg(t, "test_idempotent_collection")

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

// TestNewPool_Failure verifies that NewPool returns an error when Qdrant is
// unreachable (HealthCheck fails with connection refused).
func TestNewPool_Failure(t *testing.T) {
	cfg := unreachableCfg()

	_, err := localqdrant.NewPool(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
}
