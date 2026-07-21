package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/embedding"
	"github.com/jpgomesr/neuralvault/api/internal/embedding/ollama"
)

// validTagsHandler serves a /api/tags response listing "nomic-embed-text:latest",
// matching the default tag Ollama assigns when a model is pulled without an
// explicit version.
func validTagsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"models": []map[string]string{{"name": "nomic-embed-text:latest"}},
	})
}

// newTestClient builds an httptest server serving a valid /api/tags response
// and the given embed handler at /api/embed, then constructs a real
// ollama.Client against it via ollama.New.
func newTestClient(t *testing.T, embedHandler http.HandlerFunc) *ollama.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", validTagsHandler)
	mux.HandleFunc("/api/embed", embedHandler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: "nomic-embed-text"}}
	client, err := ollama.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}
	return client
}

// newTestClientWithModel is like newTestClient but configures a non-default
// embedding model, serving a matching /api/tags response so ollama.New's
// availability check succeeds.
func newTestClientWithModel(t *testing.T, model string, embedHandler http.HandlerFunc) *ollama.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": model + ":latest"}},
		})
	})
	mux.HandleFunc("/api/embed", embedHandler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: model}}
	client, err := ollama.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}
	return client
}

func TestEmbed_Success(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1, 0.2, 0.3}},
		})
	})

	vector, err := client.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	want := []float32{0.1, 0.2, 0.3}
	if len(vector) != len(want) {
		t.Fatalf("expected vector %v, got %v", want, vector)
	}
	for i := range want {
		if vector[i] != want[i] {
			t.Fatalf("expected vector %v, got %v", want, vector)
		}
	}
}

func TestEmbed_RequestBody(t *testing.T) {
	var gotBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1}},
		})
	})

	if _, err := client.Embed(context.Background(), "hello world"); err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if gotBody["model"] != "nomic-embed-text" {
		t.Fatalf("expected model %q, got %v", "nomic-embed-text", gotBody["model"])
	}
	input, ok := gotBody["input"].([]any)
	if !ok || len(input) != 1 || input[0] != "search_query: hello world" {
		t.Fatalf("expected input [%q], got %v", "search_query: hello world", gotBody["input"])
	}
}

func TestEmbedBatch_PrefixesDocumentsForNomic(t *testing.T) {
	var gotBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1}, {0.2}},
		})
	})

	chunks := []embedding.Chunk{
		{ID: "a", Text: "first"},
		{ID: "b", Text: "second"},
	}
	if _, err := client.EmbedBatch(context.Background(), chunks); err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}

	input, ok := gotBody["input"].([]any)
	want := []string{"search_document: first", "search_document: second"}
	if !ok || len(input) != len(want) {
		t.Fatalf("expected input %v, got %v", want, gotBody["input"])
	}
	for i, w := range want {
		if input[i] != w {
			t.Fatalf("index %d: expected %q, got %v", i, w, input[i])
		}
	}
}

func TestEmbed_DoesNotPrefixNonNomicModels(t *testing.T) {
	var gotBody map[string]any
	client := newTestClientWithModel(t, "mxbai-embed-large", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1}},
		})
	})

	if _, err := client.Embed(context.Background(), "hello world"); err != nil {
		t.Fatalf("Embed: %v", err)
	}

	input, ok := gotBody["input"].([]any)
	if !ok || len(input) != 1 || input[0] != "hello world" {
		t.Fatalf("expected unprefixed input [%q], got %v", "hello world", gotBody["input"])
	}
}

func TestEmbedBatch_DoesNotPrefixNonNomicModels(t *testing.T) {
	var gotBody map[string]any
	client := newTestClientWithModel(t, "mxbai-embed-large", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1}, {0.2}},
		})
	})

	chunks := []embedding.Chunk{
		{ID: "a", Text: "first"},
		{ID: "b", Text: "second"},
	}
	if _, err := client.EmbedBatch(context.Background(), chunks); err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}

	input, ok := gotBody["input"].([]any)
	want := []string{"first", "second"}
	if !ok || len(input) != len(want) {
		t.Fatalf("expected unprefixed input %v, got %v", want, gotBody["input"])
	}
	for i, w := range want {
		if input[i] != w {
			t.Fatalf("index %d: expected unprefixed %q, got %v", i, w, input[i])
		}
	}
}

func TestHealthCheck_Reachable(t *testing.T) {
	// newTestClient already serves a valid /api/tags response, so the server is
	// reachable and HealthCheck should succeed.
	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {})

	if err := client.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestHealthCheck_Unreachable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", validTagsHandler)
	srv := httptest.NewServer(mux)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: "nomic-embed-text"}}
	client, err := ollama.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}
	srv.Close() // server now refuses connections

	if err := client.HealthCheck(context.Background()); err == nil {
		t.Fatal("expected an error when the Ollama server is unreachable")
	}
}

func TestHealthCheck_UnexpectedStatus(t *testing.T) {
	// The first /api/tags request (during ollama.New) must succeed so
	// construction passes; the second (during HealthCheck) returns a 500 to
	// exercise the "unexpected status" branch.
	requests := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			validTagsHandler(w, r)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: "nomic-embed-text"}}
	client, err := ollama.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}

	if err := client.HealthCheck(context.Background()); err == nil {
		t.Fatal("expected an error when Ollama returns a non-200 status")
	}
}

func TestEmbedBatch_Success(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{1, 1}, {2, 2}, {3, 3}},
		})
	})

	chunks := []embedding.Chunk{
		{ID: "a", Text: "first"},
		{ID: "b", Text: "second"},
		{ID: "c", Text: "third"},
	}
	got, err := client.EmbedBatch(context.Background(), chunks)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 embeddings, got %d", len(got))
	}
	wantIDs := []string{"a", "b", "c"}
	wantVectors := [][]float32{{1, 1}, {2, 2}, {3, 3}}
	for i, e := range got {
		if e.ChunkID != wantIDs[i] {
			t.Fatalf("index %d: expected ChunkID %q, got %q", i, wantIDs[i], e.ChunkID)
		}
		if e.Vector[0] != wantVectors[i][0] || e.Vector[1] != wantVectors[i][1] {
			t.Fatalf("index %d: expected vector %v, got %v", i, wantVectors[i], e.Vector)
		}
	}
}

func TestEmbedBatch_Empty(t *testing.T) {
	called := false
	client := newTestClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})

	got, err := client.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %v", got)
	}
	if called {
		t.Fatal("expected /api/embed to not be called for an empty batch")
	}
}

func TestEmbed_NonOKStatus_WithErrorBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "model not found"})
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "model not found") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error mentioning status 500 and %q, got: %v", "model not found", err)
	}
}

func TestEmbed_NonOKStatus_NoBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected error mentioning status 503, got: %v", err)
	}
}

func TestEmbed_MalformedJSON(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "decoding ollama embed response") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}

func TestEmbed_MismatchedEmbeddingCount(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1}, {0.2}},
		})
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "expected 1 embeddings, got 2") {
		t.Fatalf("expected count-mismatch error, got: %v", err)
	}
}

func TestEmbedBatch_MismatchedEmbeddingCount(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1}},
		})
	})

	chunks := []embedding.Chunk{{ID: "a", Text: "1"}, {ID: "b", Text: "2"}, {ID: "c", Text: "3"}}
	_, err := client.EmbedBatch(context.Background(), chunks)
	if err == nil || !strings.Contains(err.Error(), "expected 3 embeddings, got 1") {
		t.Fatalf("expected count-mismatch error, got: %v", err)
	}
}

func TestEmbed_EmptyVector(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{}},
		})
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "embedding at index 0 is empty") {
		t.Fatalf("expected empty-vector error, got: %v", err)
	}
}

func TestEmbedBatch_EmptyVectorAtNonZeroIndex(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1}, {}, {0.3}},
		})
	})

	chunks := []embedding.Chunk{{ID: "a", Text: "1"}, {ID: "b", Text: "2"}, {ID: "c", Text: "3"}}
	_, err := client.EmbedBatch(context.Background(), chunks)
	if err == nil || !strings.Contains(err.Error(), "embedding at index 1 is empty") {
		t.Fatalf("expected empty-vector error at index 1, got: %v", err)
	}
}

func TestEmbed_NetworkError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", validTagsHandler)
	srv := httptest.NewServer(mux)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: "nomic-embed-text"}}
	client, err := ollama.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}
	srv.Close() // force connection errors on subsequent requests

	_, err = client.Embed(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "calling ollama embed endpoint") {
		t.Fatalf("expected network error, got: %v", err)
	}
}

func TestEmbed_ContextCanceled(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{0.1}}})
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Embed(ctx, "hello")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context.Canceled, got: %v", err)
	}
}

func TestNew_ModelNotAvailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "llama3:latest"}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: "nomic-embed-text"}}
	_, err := ollama.New(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "ollama pull nomic-embed-text") {
		t.Fatalf("expected model-not-found error, got: %v", err)
	}
}

func TestNew_TagsEndpointUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NewServeMux())
	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: "nomic-embed-text"}}
	srv.Close()

	_, err := ollama.New(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for unreachable ollama server, got nil")
	}
}

func TestNew_TagsEndpointNon200(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: "nomic-embed-text"}}
	_, err := ollama.New(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error mentioning status 500, got: %v", err)
	}
}

func TestNew_ModelNameMatchesTagSuffix(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", validTagsHandler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: "nomic-embed-text"}}
	if _, err := ollama.New(context.Background(), cfg); err != nil {
		t.Fatalf("expected New to succeed matching %q against tag %q, got: %v",
			"nomic-embed-text", "nomic-embed-text:latest", err)
	}
}

// TestNew_ModelNameExactMatch covers the bare-name side of the "exact or
// :latest-suffixed" match in ensureModelAvailable, in case a server ever
// reports a tag with no suffix at all.
func TestNew_ModelNameExactMatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "nomic-embed-text"}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: "nomic-embed-text"}}
	if _, err := ollama.New(context.Background(), cfg); err != nil {
		t.Fatalf("expected New to succeed matching %q exactly, got: %v", "nomic-embed-text", err)
	}
}

func TestNew_TagsMalformedJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, EmbeddingModel: "nomic-embed-text"}}
	_, err := ollama.New(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "decoding ollama tags response") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}
