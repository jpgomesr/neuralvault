package openaicompat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpgomesr/neuralvault/api/internal/embedding"
	"github.com/jpgomesr/neuralvault/api/internal/embedding/openaicompat"
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

func TestHealthCheck_Success(t *testing.T) {
	client := newTestClient(t, "/models", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		w.WriteHeader(http.StatusOK)
	})

	if err := client.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestHealthCheck_ErrorStatus(t *testing.T) {
	client := newTestClient(t, "/models", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	if err := client.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck with bad key = nil error, want error")
	}
}

func TestHealthCheck_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	client := openaicompat.New("gemini", srv.URL, "test-key", "text-embedding-004")
	srv.Close()

	if err := client.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck against a closed server = nil error, want a connection error")
	}
}

func TestHealthCheck_InvalidBaseURL(t *testing.T) {
	client := openaicompat.New("gemini", "://bad-url", "test-key", "text-embedding-004")

	if err := client.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck with an invalid base URL = nil error, want an error")
	}
}

func TestEmbed_InvalidBaseURL(t *testing.T) {
	client := openaicompat.New("gemini", "://bad-url", "test-key", "text-embedding-004")

	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed with an invalid base URL = nil error, want an error")
	}
}

func TestEmbed_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	client := openaicompat.New("gemini", srv.URL, "test-key", "text-embedding-004")
	srv.Close()

	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed against a closed server = nil error, want a connection error")
	}
}

// TestEmbed_ErrorBodyWithNoMessage covers the non-200 fallback: a response
// carrying no "error.message" still needs to surface the HTTP status.
func TestEmbed_ErrorBodyWithNoMessage(t *testing.T) {
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed = nil error, want an error for a 503 with no body")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %v, want it to mention the status code", err)
	}
}

func TestEmbed_MalformedResponseBody(t *testing.T) {
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "not json")
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed = nil error, want a decode error")
	}
}

// TestEmbed_EmptyVector covers toVectors' guard against a provider returning
// a zero-length embedding: silently accepting it would corrupt the Qdrant
// collection's vector size on the very first upsert.
func TestEmbed_EmptyVector(t *testing.T) {
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []float32{}}},
		})
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed with an empty embedding = nil error, want error")
	}
}

// TestEmbed_RetriesOn429 covers the happy retry path: a transient rate limit
// resolves within maxRetries, so the caller gets a result instead of an error.
func TestEmbed_RetriesOn429(t *testing.T) {
	var calls int
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"message": "rate limited"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []float32{0.1}}},
		})
	})

	vector, err := client.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vector) != 1 {
		t.Fatalf("len(vector) = %d, want 1", len(vector))
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

// TestEmbed_GivesUpAfterMaxRetries covers a 429 that never clears (e.g. a
// daily quota, not a transient per-minute limit): the client must eventually
// surface the error instead of retrying forever.
func TestEmbed_GivesUpAfterMaxRetries(t *testing.T) {
	var calls int
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "quota exceeded"},
		})
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed with sustained 429s = nil error, want error")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("error = %v, want it to carry the provider message", err)
	}
	const wantCalls = 5 // one initial attempt + maxRetries retries
	if calls != wantCalls {
		t.Errorf("calls = %d, want %d", calls, wantCalls)
	}
}

// TestEmbed_429AbortsOnContextCancellation covers that a retry backoff
// doesn't outlive the caller's own context deadline.
func TestEmbed_429AbortsOnContextCancellation(t *testing.T) {
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Embed(ctx, "hello")
	if err == nil {
		t.Fatal("Embed with a canceled context = nil error, want error")
	}
}

// TestEmbed_NonRetryableErrorStatus covers that a non-429 error status is not
// retried, so a persistent 401 fails fast instead of waiting through backoff.
func TestEmbed_NonRetryableErrorStatus(t *testing.T) {
	var calls int
	client := newTestClient(t, "/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed with a 401 = nil error, want error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry on non-429 status)", calls)
	}
}

var _ embedding.Embedder = (*openaicompat.Client)(nil)
