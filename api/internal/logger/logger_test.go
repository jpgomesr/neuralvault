package logger_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jpgomesr/NeuralVault/internal/logger"
)

func TestRequestID_NoMiddleware(t *testing.T) {
	if got := logger.RequestID(context.Background()); got != "" {
		t.Errorf("RequestID() = %q, want empty string when no request ID is set", got)
	}
}

func TestRequestID_WithMiddleware(t *testing.T) {
	var got string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = logger.RequestID(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	middleware.RequestID(next).ServeHTTP(httptest.NewRecorder(), req)

	if got == "" {
		t.Error("RequestID() = empty string, want the ID set by middleware.RequestID")
	}
}
