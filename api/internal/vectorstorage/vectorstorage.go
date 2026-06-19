package vectorstorage

import (
	"context"

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

// NewClient creates and returns a Client backed by Qdrant.
func NewClient(ctx context.Context, cfg *config.Config) (Client, error) {
	return qdrant.NewPool(ctx, cfg)
}
