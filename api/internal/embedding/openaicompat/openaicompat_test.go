package openaicompat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/embedding/openaicompat"
)

func newTestClient(t *testing.T, path string, handler http.HandlerFunc) *openaicompat.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return openaicompat.New("gemini", srv.URL, "test-key", "text-embedding-004")
}

func TestEmbed(t *testing.T) {
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": []float32{0.1, 0.2, 0.3}},
			},
		})
	})

	vector, err := client.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vector) != 3 {
		t.Fatalf("len(vector) = %d, want 3", len(vector))
	}
}

// The OpenAI schema permits the provider to return embeddings out of order, so
// they must be re-sorted by index — otherwise vectors would be silently
// attached to the wrong chunks.
func TestEmbedBatch_ReordersByIndex(t *testing.T) {
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 1, "embedding": []float32{2}},
				{"index": 0, "embedding": []float32{1}},
			},
		})
	})

	const first, second = "chunk-1", "chunk-2"
	got, err := client.EmbedBatch(context.Background(), []embedding.Chunk{
		{ID: first, Text: "one"},
		{ID: second, Text: "two"},
	})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}

	if got[0].ChunkID != first || got[0].Vector[0] != 1 {
		t.Errorf("got[0] = %+v, want the index-0 vector on the first chunk", got[0])
	}
	if got[1].ChunkID != second || got[1].Vector[0] != 2 {
		t.Errorf("got[1] = %+v, want the index-1 vector on the second chunk", got[1])
	}
}

func TestEmbedBatch_Empty(t *testing.T) {
	client := newTestClient(t, "/embeddings", func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be made for an empty batch")
	})

	got, err := client.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// A short response would otherwise leave chunks holding nil vectors, which
// Qdrant would reject far from the cause.
func TestEmbedBatch_CountMismatch(t *testing.T) {
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []float32{1}}},
		})
	})

	_, err := client.EmbedBatch(context.Background(), []embedding.Chunk{
		{ID: "chunk-1", Text: "one"},
		{ID: "chunk-2", Text: "two"},
	})
	if err == nil {
		t.Fatal("EmbedBatch with a short response = nil error, want error")
	}
}

func TestEmbed_InvalidKey(t *testing.T) {
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "API key not valid"},
		})
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed with bad key = nil error, want error")
	}
	if !strings.Contains(err.Error(), "API key not valid") {
		t.Errorf("error = %v, want it to carry the provider message", err)
	}
}

var _ embedding.Embedder = (*openaicompat.Client)(nil)
