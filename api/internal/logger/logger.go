package logger

import (
	"log/slog"
	"os"
)

func Init(level slog.Level) {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
	})

	slog.SetDefault(slog.New(handler))
}
