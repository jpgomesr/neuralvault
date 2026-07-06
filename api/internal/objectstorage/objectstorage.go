// Package objectstorage defines the Client interface for object storage
// and a factory that returns a MinIO-backed implementation.
package objectstorage

import (
	"context"
	"io"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage/minio"
)

// Client stores and retrieves objects. Implementations must be safe for concurrent use.
type Client interface {
	Upload(ctx context.Context, key string, r io.Reader, size int64) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	// ListObjects returns the keys of all objects whose key starts with prefix.
	ListObjects(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
	// HealthCheck verifies the object store is reachable and the configured
	// bucket is accessible with the current credentials.
	HealthCheck(ctx context.Context) error
}

// NewClient returns a MinIO-backed Client configured from cfg.
func NewClient(ctx context.Context, cfg *config.Config) (Client, error) {
	return minio.New(ctx, cfg)
}
