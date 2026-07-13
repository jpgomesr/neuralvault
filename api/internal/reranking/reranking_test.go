package reranking_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/reranking"
)

// NewReranker is a thin factory over the tei provider; these tests cover the
// factory itself (the provider's own behaviour is tested in internal/reranking/tei).

func TestNewReranker_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model_id": "BAAI/bge-reranker-base"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Reranker: config.Reranker{URL: srv.URL, Model: "BAAI/bge-reranker-base"}}
	r, err := reranking.NewReranker(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewReranker: %v", err)
	}
	if r == nil {
		t.Fatal("expected a non-nil Reranker")
	}
}

func TestNewReranker_ProviderError(t *testing.T) {
	// An unreachable TEI server makes the provider's fail-fast model check fail,
	// so the factory must surface that error rather than return a broken client.
	cfg := &config.Config{Reranker: config.Reranker{URL: "http://127.0.0.1:1", Model: "BAAI/bge-reranker-base"}}
	if _, err := reranking.NewReranker(context.Background(), cfg); err == nil {
		t.Fatal("expected an error when the reranker is unreachable")
	}
}
