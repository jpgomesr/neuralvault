package vectorstorage

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage/qdrant"
	qdrantpb "github.com/qdrant/go-client/qdrant"
)

// Client is the interface that wraps the vector database client.
// Concrete implementations must satisfy this interface.
//
// Note: method signatures use qdrant protobuf types since the project
// uses Qdrant as its vector store. Swap the implementation in NewClient
// without changing this interface if the types remain compatible.
type Client interface {
	HealthCheck(ctx context.Context) (*qdrantpb.HealthCheckReply, error)
	CollectionExists(ctx context.Context, collectionName string) (bool, error)
	CreateCollection(ctx context.Context, request *qdrantpb.CreateCollection) error
	DeleteCollection(ctx context.Context, collectionName string) error
	Upsert(ctx context.Context, request *qdrantpb.UpsertPoints) (*qdrantpb.UpdateResult, error)
	Query(ctx context.Context, request *qdrantpb.QueryPoints) ([]*qdrantpb.ScoredPoint, error)
	Delete(ctx context.Context, request *qdrantpb.DeletePoints) (*qdrantpb.UpdateResult, error)
	Count(ctx context.Context, request *qdrantpb.CountPoints) (uint64, error)
	Close() error
}

// NewClient creates a Client backed by Qdrant and ensures the default
// collection exists.
//
// The default collection is the one workspaces on the server's environment
// default embedding model use. A workspace that brings its own embedding
// provider gets its own collection instead, created on demand by
// EnsureCollection when the setting is saved.
func NewClient(ctx context.Context, cfg *config.Config) (Client, error) {
	client, err := qdrant.NewPool(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := EnsureCollection(ctx, client, cfg.Qdrant.CollectionName, cfg.Qdrant.VectorSize); err != nil {
		return nil, err
	}

	return client, nil
}

// EnsureCollection creates a cosine-distance collection of the given vector
// size if it does not already exist.
//
// It does not reconcile an existing collection's vector size — Qdrant fixes it
// at creation, and changing it means dropping the data. Callers must therefore
// give each embedding model its own collection name rather than reusing one
// across models; see modelconfig.collectionName, which derives the name from
// the model and its dimension so a size mismatch cannot arise.
func EnsureCollection(ctx context.Context, client Client, name string, vectorSize uint64) error {
	exists, err := client.CollectionExists(ctx, name)
	if err != nil {
		return fmt.Errorf("checking qdrant collection: %w", err)
	}
	if exists {
		return nil
	}

	err = client.CreateCollection(ctx, &qdrantpb.CreateCollection{
		CollectionName: name,
		VectorsConfig: qdrantpb.NewVectorsConfig(&qdrantpb.VectorParams{
			Size:     vectorSize,
			Distance: qdrantpb.Distance_Cosine,
		}),
	})
	if err != nil {
		return fmt.Errorf("creating qdrant collection: %w", err)
	}

	slog.InfoContext(ctx, "qdrant collection created", "collection", name, "vector_size", vectorSize)
	return nil
}
