package qdrant

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jpgomesr/neuralvault/api/internal/config"
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

	slog.Info("qdrant connected", "url", cfg.Qdrant.URL)
	return client, nil
}
