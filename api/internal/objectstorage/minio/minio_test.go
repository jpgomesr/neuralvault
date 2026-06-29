package minio_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	localminio "github.com/jpgomesr/NeuralVault/internal/objectstorage/minio"
)

var sharedCfg *config.Config

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "minio/minio:latest",
			ExposedPorts: []string{"9000/tcp"},
			Env: map[string]string{
				"MINIO_ROOT_USER":     "minioadmin",
				"MINIO_ROOT_PASSWORD": "minioadmin",
			},
			Cmd:        []string{"server", "/data"},
			WaitingFor: wait.ForHTTP("/minio/health/live").WithPort("9000/tcp"),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start minio container: %v\n", err)
		return 1
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	host, err := ctr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get container host: %v\n", err)
		return 1
	}
	port, err := ctr.MappedPort(ctx, "9000")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get mapped port: %v\n", err)
		return 1
	}

	sharedCfg = &config.Config{
		MinIO: config.MinIO{
			Endpoint:  fmt.Sprintf("%s:%d", host, port.Num()),
			AccessKey: "minioadmin",
			SecretKey: "minioadmin",
			Bucket:    "neuralvault-test",
			UseSSL:    false,
		},
	}

	return m.Run()
}

// unreachableCfg returns a config pointing at a port with nothing listening.
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

func TestNew_Success(t *testing.T) {
	_, err := localminio.New(context.Background(), sharedCfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// TestNew_BucketAlreadyExists covers the idempotent ensureBucket path.
func TestNew_BucketAlreadyExists(t *testing.T) {
	if _, err := localminio.New(context.Background(), sharedCfg); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := localminio.New(context.Background(), sharedCfg); err != nil {
		t.Fatalf("second call (bucket already exists): %v", err)
	}
}

func TestNew_Unreachable(t *testing.T) {
	_, err := localminio.New(context.Background(), unreachableCfg())
	if err == nil {
		t.Fatal("expected error for unreachable endpoint, got nil")
	}
}

// TestNewClient_Factory verifies the objectstorage.NewClient factory and
// that the returned value satisfies the Client interface.
func TestNewClient_Factory(t *testing.T) {
	client, err := objectstorage.NewClient(context.Background(), sharedCfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.ListObjects(context.Background(), "factory-test/"); err != nil {
		t.Fatalf("ListObjects via factory client: %v", err)
	}
}

func TestClient_UploadDownloadDelete(t *testing.T) {
	c, err := localminio.New(context.Background(), sharedCfg)
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
	defer rc.Close() //nolint:errcheck

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

func TestClient_ListObjects(t *testing.T) {
	c, err := localminio.New(context.Background(), sharedCfg)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	ctx := context.Background()
	prefix := "test/list-objects/"
	wantKeys := []string{prefix + "alpha.md", prefix + "beta.md", prefix + "gamma.md"}
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
			t.Errorf("key[%d]: want %q, got %q", i, k, gotKeys[i])
		}
	}
}

func TestClient_ListObjects_EmptyPrefix(t *testing.T) {
	c, err := localminio.New(context.Background(), sharedCfg)
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
