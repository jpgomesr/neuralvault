package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/jpgomesr/NeuralVault/internal/auth"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	"github.com/jpgomesr/NeuralVault/internal/router"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage"
)

// @title NeuralVault API
// @version 0.0.1
// @description API for NeuralVault
// @BasePath /
func main() {
	logger.Init(slog.LevelDebug)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()

	pgPool, err := storage.NewPool(ctx, *cfg)
	if err != nil {
		slog.Error("failed to connect to postgres", "err", err)
		os.Exit(1)
	}
	defer pgPool.Close()

	qdrantClient, err := vectorstorage.NewClient(ctx, cfg)
	if err != nil {
		slog.Error("failed to connect to qdrant", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := qdrantClient.Close(); err != nil {
			slog.Error("failed to close qdrant", "err", err)
		}
	}()

	minioClient, err := objectstorage.NewClient(ctx, cfg)
	if err != nil {
		slog.Error("failed to connect to minio", "err", err)
		os.Exit(1)
	}

	embedder, err := embedding.NewEmbedder(ctx, cfg)
	if err != nil {
		slog.Error("failed to initialise embedder", "err", err)
		os.Exit(1)
	}

	llmProvider, err := llm.NewProvider(ctx, cfg)
	if err != nil {
		slog.Error("failed to initialise llm provider", "err", err)
		os.Exit(1)
	}

	authService, err := auth.NewAuthService(ctx, cfg, pgPool)
	if err != nil {
		slog.Error("failed to initialise auth service", "err", err)
		os.Exit(1)
	}

	r := router.NewRouter(cfg, pgPool, minioClient, embedder, qdrantClient, llmProvider, authService)

	if err := startHTTPServer(cfg, r, http.ListenAndServe); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func startHTTPServer(cfg *config.Config, handler http.Handler, listenAndServe func(string, http.Handler) error) error {
	addr := fmt.Sprintf(":%d", cfg.Server.Port)

	slog.Info("server started", "addr", addr)

	return listenAndServe(addr, handler)
}
