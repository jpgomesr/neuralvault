package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/router"
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

	r := router.NewRouter(cfg)

	addr := ":8080"

	slog.Info("server started", "addr", addr)

	if err := http.ListenAndServe(addr, r); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
