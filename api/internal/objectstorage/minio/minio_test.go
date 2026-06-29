package minio_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"sort"
	"testing"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	localminio "github.com/jpgomesr/NeuralVault/internal/objectstorage/minio"
)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// integrationCfg returns a Config pointing at a live MinIO instance.
// The test is skipped when MINIO_ENDPOINT is not set.
func integrationCfg(t *testing.T) *config.Config {
	t.Helper()
	if os.Getenv("MINIO_ENDPOINT") == "" {
		t.Skip("MINIO_ENDPOINT not set; skipping MinIO integration test")
	}
	return &config.Config{
		MinIO: config.MinIO{
			Endpoint:  envOrDefault("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey: envOrDefault("MINIO_ACCESS_KEY", "minioadmin"),
			SecretKey: envOrDefault("MINIO_SECRET_KEY", "minioadmin"),
			Bucket:    "neuralvault-test",
			UseSSL:    false,
		},
	}
}

// unreachableCfg returns a Config pointing at a port that has nothing listening.
func unreachableCfg() *config.Config {
	return &config.Config{
		MinIO: config.MinIO{
			Endpoint:  "localhost:59998",
			AccessKey: "minioadmin",
			SecretKey: "minioadmin",
			Bucket:    "neuralvault-test",
			UseSSL:    false,
		},
	}
}

// TestNew_Success verifies that New connects and creates the bucket.
func TestNew_Success(t *testing.T) {
	cfg := integrationCfg(t)
	_, err := localminio.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// TestNew_BucketAlreadyExists covers the idempotent ensureBucket path.
func TestNew_BucketAlreadyExists(t *testing.T) {
	cfg := integrationCfg(t)
	if _, err := localminio.New(context.Background(), cfg); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := localminio.New(context.Background(), cfg); err != nil {
		t.Fatalf("second call (bucket already exists): %v", err)
	}
}

// TestNew_Unreachable verifies that New fails when MinIO is unreachable.
func TestNew_Unreachable(t *testing.T) {
	_, err := localminio.New(context.Background(), unreachableCfg())
	if err == nil {
		t.Fatal("expected error for unreachable endpoint, got nil")
	}
}

// TestNewClient_Factory verifies the objectstorage.NewClient factory covers the
// two-line wrapper and returns a Client that satisfies the interface.
func TestNewClient_Factory(t *testing.T) {
	cfg := integrationCfg(t)
	client, err := objectstorage.NewClient(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// Verify interface satisfaction by calling a method.
	_, err = client.ListObjects(context.Background(), "factory-test/")
	if err != nil {
		t.Fatalf("ListObjects via factory client: %v", err)
	}
}

// TestClient_UploadDownloadDelete verifies the happy-path round-trip.
func TestClient_UploadDownloadDelete(t *testing.T) {
	cfg := integrationCfg(t)
	c, err := localminio.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	ctx := context.Background()
	key := "test/upload-download-delete.txt"
	content := []byte("hello neuralvault")

	if err := c.Upload(ctx, key, bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	t.Cleanup(func() { _ = c.Delete(ctx, key) })

	rc, err := c.Download(ctx, key)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: want %q, got %q", content, got)
	}

	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// TestClient_ListObjects verifies prefix-filtered listing returns the right keys.
func TestClient_ListObjects(t *testing.T) {
	cfg := integrationCfg(t)
	c, err := localminio.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	ctx := context.Background()
	prefix := "test/list-objects/"
	wantKeys := []string{
		prefix + "alpha.md",
		prefix + "beta.md",
		prefix + "gamma.md",
	}
	content := []byte("x")

	for _, key := range wantKeys {
		key := key
		if err := c.Upload(ctx, key, bytes.NewReader(content), int64(len(content))); err != nil {
			t.Fatalf("Upload %q: %v", key, err)
		}
		t.Cleanup(func() { _ = c.Delete(ctx, key) })
	}

	gotKeys, err := c.ListObjects(ctx, prefix)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("expected %d keys, got %d: %v", len(wantKeys), len(gotKeys), gotKeys)
	}

	sort.Strings(gotKeys)
	sort.Strings(wantKeys)
	for i, k := range wantKeys {
		if gotKeys[i] != k {
			t.Errorf("key[%d]: expected %q, got %q", i, k, gotKeys[i])
		}
	}
}

// TestClient_ListObjects_EmptyPrefix verifies an empty result for an unknown prefix.
func TestClient_ListObjects_EmptyPrefix(t *testing.T) {
	cfg := integrationCfg(t)
	c, err := localminio.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	keys, err := c.ListObjects(context.Background(), "nonexistent-prefix-xyz/")
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d: %v", len(keys), keys)
	}
}
