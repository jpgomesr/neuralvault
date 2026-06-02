package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/router"
)

func main() {
	logger.Init(slog.LevelDebug)

	r := router.NewRouter()

	addr := ":8080"

	slog.Info("server started", "addr", addr)

	if err := http.ListenAndServe(addr, r); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
