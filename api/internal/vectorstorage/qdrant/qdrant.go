package qdrant

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/qdrant/go-client/qdrant"
)

func NewPool(ctx context.Context, cfg *config.Config) (*qdrant.Client, error) {
	client, err := qdrant.NewClient(&qdrant.Config{
		Host:   cfg.Qdrant.URL,
		Port:   cfg.Qdrant.GrpcPort,
		APIKey: cfg.Qdrant.APIKey,
		UseTLS: cfg.Qdrant.UseTLS,
	})
	if err != nil {
		return nil, fmt.Errorf("creating qdrant client: %w", err)
	}

	if _, err := client.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("qdrant health check: %w", err)
	}

	if err := ensureCollection(ctx, client, cfg); err != nil {
		return nil, err
	}

	slog.Info("qdrant connected", "url", cfg.Qdrant.URL, "collection", cfg.Qdrant.CollectionName)
	return client, nil
}

func ensureCollection(ctx context.Context, client *qdrant.Client, cfg *config.Config) error {
	exists, err := client.CollectionExists(ctx, cfg.Qdrant.CollectionName)
	if err != nil {
		return fmt.Errorf("checking qdrant collection: %w", err)
	}
	if exists {
		return nil
	}

	err = client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: cfg.Qdrant.CollectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     cfg.Qdrant.VectorSize,
			Distance: qdrant.Distance_Cosine,
		}),
	})
	if err != nil {
		return fmt.Errorf("creating qdrant collection: %w", err)
	}

	slog.Info("qdrant collection created", "collection", cfg.Qdrant.CollectionName)
	return nil
}
