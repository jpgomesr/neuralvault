package logger

import (
	"context"
	"log/slog"
	"os"

	"github.com/go-chi/chi/v5/middleware"
)

func Init(level slog.Level) {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
	})

	slog.SetDefault(slog.New(handler))
}

// RequestID returns the request-scoped ID set by the router's RequestID
// middleware, or "" if ctx carries none (e.g. background goroutines, tests).
func RequestID(ctx context.Context) string {
	return middleware.GetReqID(ctx)
}
