package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jpgomesr/NeuralVault/internal/auth"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/objectstorage"
	"github.com/jpgomesr/NeuralVault/internal/reranking"
	"github.com/jpgomesr/NeuralVault/internal/router"
	"github.com/jpgomesr/NeuralVault/internal/sources"
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

	// Any source still in indexing status is a leftover from a crashed or
	// restarted process, since indexing only runs in-process. Reset them to error
	// before serving so SSE clients get a terminal event instead of waiting out
	// the stream timeout. Best-effort: a failure here must not block startup.
	if n, err := sources.ResetStuckIndexing(ctx, pgPool); err != nil {
		slog.Error("failed to reset stuck indexing sources", "err", err)
	} else if n > 0 {
		slog.Info("reset sources stuck in indexing", "count", n)
	}

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

	reranker, err := reranking.NewReranker(ctx, cfg)
	if err != nil {
		slog.Error("failed to initialise reranker", "err", err)
		os.Exit(1)
	}

	authService, err := auth.NewAuthService(ctx, cfg, pgPool)
	if err != nil {
		slog.Error("failed to initialise auth service", "err", err)
		os.Exit(1)
	}

	r := router.NewRouter(cfg, pgPool, minioClient, embedder, qdrantClient, llmProvider, reranker, authService)

	// ctx is cancelled on SIGINT/SIGTERM to trigger graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serve := func(s *http.Server) error { return s.ListenAndServe() }
	shutdown := func(s *http.Server, c context.Context) error { return s.Shutdown(c) }

	if err := startHTTPServer(ctx, cfg, r, serve, shutdown); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// startHTTPServer builds an http.Server with the configured timeouts and runs it
// until ctx is cancelled (SIGINT/SIGTERM), then drains in-flight connections via
// shutdown, bounded by cfg.Server.ShutdownTimeout. serve and shutdown are injected
// so the lifecycle can be unit-tested without binding a real socket.
func startHTTPServer(
	ctx context.Context,
	cfg *config.Config,
	handler http.Handler,
	serve func(*http.Server) error,
	shutdown func(*http.Server, context.Context) error,
) error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           handler,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("server started", "addr", srv.Addr)
		serveErr <- serve(srv)
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining connections", "timeout", cfg.Server.ShutdownTimeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()

		if err := shutdown(srv, shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}

		slog.Info("server shut down gracefully")
		return nil
	}
}
