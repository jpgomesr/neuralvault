package tei_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/reranking/tei"
	"github.com/jpgomesr/neuralvault/api/internal/reranking/types"
)

// validInfoHandler serves a /info response matching the default test model.
func validInfoHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"model_id": "BAAI/bge-reranker-base"})
}

// newTestClient builds an httptest server serving a valid /info response and
// the given rerank handler at /rerank, then constructs a real tei.Client
// against it via tei.New.
func newTestClient(t *testing.T, rerankHandler http.HandlerFunc) *tei.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/info", validInfoHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/rerank", rerankHandler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Reranker: config.Reranker{URL: srv.URL, Model: "BAAI/bge-reranker-base"}}
	client, err := tei.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("tei.New: %v", err)
	}
	return client
}

func TestNew_Success(t *testing.T) {
	newTestClient(t, func(http.ResponseWriter, *http.Request) {})
}

func TestNew_ModelMismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model_id": "some-other-model"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Reranker: config.Reranker{URL: srv.URL, Model: "BAAI/bge-reranker-base"}}
	_, err := tei.New(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "serving model") {
		t.Fatalf("expected model mismatch error, got: %v", err)
	}
}

func TestNew_InfoEndpointUnreachable(t *testing.T) {
	cfg := &config.Config{Reranker: config.Reranker{URL: "http://127.0.0.1:1", Model: "BAAI/bge-reranker-base"}}
	_, err := tei.New(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected an error when the reranker is unreachable")
	}
}

func TestNew_InfoEndpointNon200(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Reranker: config.Reranker{URL: srv.URL, Model: "BAAI/bge-reranker-base"}}
	_, err := tei.New(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "info check") {
		t.Fatalf("expected info check error, got: %v", err)
	}
}

func TestNew_InfoDecodeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Reranker: config.Reranker{URL: srv.URL, Model: "BAAI/bge-reranker-base"}}
	_, err := tei.New(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "decoding reranker info") {
		t.Fatalf("expected info decode error, got: %v", err)
	}
}

func TestRerank_Success(t *testing.T) {
	var gotBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		// TEI returns results out of input order, sorted by score descending —
		// exercise that the client re-maps by index rather than assuming order.
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"index": 1, "score": 0.9},
			{"index": 0, "score": 0.2},
		})
	})

	candidates := []types.Candidate{
		{ID: "chunk-a", Text: "first candidate"},
		{ID: "chunk-b", Text: "second candidate"},
	}
	results, err := client.Rerank(context.Background(), "my query", candidates)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}

	if gotBody["query"] != "my query" {
		t.Errorf("expected query %q, got %v", "my query", gotBody["query"])
	}
	texts, ok := gotBody["texts"].([]any)
	if !ok || len(texts) != 2 || texts[0] != "first candidate" || texts[1] != "second candidate" {
		t.Fatalf("expected texts [%q, %q], got %v", "first candidate", "second candidate", gotBody["texts"])
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].CandidateID != "chunk-b" || results[0].Score != 0.9 {
		t.Errorf("results[0] = %+v, want CandidateID=chunk-b Score=0.9", results[0])
	}
	if results[1].CandidateID != "chunk-a" || results[1].Score != 0.2 {
		t.Errorf("results[1] = %+v, want CandidateID=chunk-a Score=0.2", results[1])
	}
}

func TestRerank_Empty(t *testing.T) {
	called := false
	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		called = true
	})

	results, err := client.Rerank(context.Background(), "query", nil)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %v", results)
	}
	if called {
		t.Fatal("expected /rerank to not be called for an empty candidate list")
	}
}

func TestRerank_NonOKStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "model overloaded"})
	})

	_, err := client.Rerank(context.Background(), "query", []types.Candidate{{ID: "a", Text: "x"}})
	if err == nil || !strings.Contains(err.Error(), "model overloaded") {
		t.Fatalf("expected error containing %q, got: %v", "model overloaded", err)
	}
}

func TestRerank_IndexOutOfRange(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"index": 5, "score": 0.5}})
	})

	_, err := client.Rerank(context.Background(), "query", []types.Candidate{{ID: "a", Text: "x"}})
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out of range error, got: %v", err)
	}
}

func TestRerank_NonOKStatusNoErrorBody(t *testing.T) {
	// A non-200 with no decodable {"error": ...} body falls back to the generic
	// "unexpected status" message rather than surfacing an empty error string.
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	_, err := client.Rerank(context.Background(), "query", []types.Candidate{{ID: "a", Text: "x"}})
	if err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("expected unexpected status error, got: %v", err)
	}
}

func TestRerank_DecodeError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	})

	_, err := client.Rerank(context.Background(), "query", []types.Candidate{{ID: "a", Text: "x"}})
	if err == nil || !strings.Contains(err.Error(), "decoding rerank response") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}

func TestRerank_RequestError(t *testing.T) {
	// Build a client against a server, then close it so the /rerank round-trip
	// fails at the transport level (before any status code is seen).
	mux := http.NewServeMux()
	mux.HandleFunc("/info", validInfoHandler)
	srv := httptest.NewServer(mux)

	cfg := &config.Config{Reranker: config.Reranker{URL: srv.URL, Model: "BAAI/bge-reranker-base"}}
	client, err := tei.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("tei.New: %v", err)
	}
	srv.Close() // now unreachable, /info having already succeeded during New

	_, err = client.Rerank(context.Background(), "query", []types.Candidate{{ID: "a", Text: "x"}})
	if err == nil || !strings.Contains(err.Error(), "calling reranker") {
		t.Fatalf("expected calling reranker error, got: %v", err)
	}
}

func TestHealthCheck_Non200(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", validInfoHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Reranker: config.Reranker{URL: srv.URL, Model: "BAAI/bge-reranker-base"}}
	client, err := tei.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("tei.New: %v", err)
	}

	if err := client.HealthCheck(context.Background()); err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("expected unexpected status error, got: %v", err)
	}
}

func TestHealthCheck_Reachable(t *testing.T) {
	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {})
	if err := client.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestHealthCheck_Unreachable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", validInfoHandler)
	srv := httptest.NewServer(mux)

	cfg := &config.Config{Reranker: config.Reranker{URL: srv.URL, Model: "BAAI/bge-reranker-base"}}
	client, err := tei.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("tei.New: %v", err)
	}

	srv.Close() // server is now unreachable, /info having already succeeded during New
	if err := client.HealthCheck(context.Background()); err == nil {
		t.Fatal("expected an error when the reranker is unreachable")
	}
}
