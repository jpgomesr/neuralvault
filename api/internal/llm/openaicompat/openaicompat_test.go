package openaicompat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/llm/openaicompat"
)

// newTestClient serves handler at the given path and returns a client pointed
// at it, standing in for any of the OpenAI-compatible providers (Groq, Gemini,
// OpenRouter, GitHub Models, OpenAI) — they differ only by base URL.
func newTestClient(t *testing.T, path string, handler http.HandlerFunc) *openaicompat.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return openaicompat.New("groq", srv.URL, "test-key", "llama-3.3-70b")
}

func basicRequest() llm.CompletionRequest {
	return llm.CompletionRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	}
}

// drain collects a stream into its content, its terminal error, and how many
// Done chunks it emitted — the channel contract callers depend on.
func drain(t *testing.T, chunks <-chan llm.StreamChunk) (content string, doneCount int, streamErr error) {
	t.Helper()

	var sb strings.Builder
	for chunk := range chunks {
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
		if chunk.Done {
			doneCount++
		}
		sb.WriteString(chunk.Content)
	}
	return sb.String(), doneCount, streamErr
}

func TestComplete_Success(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "llama-3.3-70b",
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "hi there"}},
			},
			"usage": map[string]any{
				"prompt_tokens":         10,
				"completion_tokens":     5,
				"total_tokens":          15,
				"prompt_tokens_details": map[string]int{"cached_tokens": 4},
			},
		})
	})

	resp, err := client.Complete(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if resp.Content != "hi there" {
		t.Errorf("Content = %q, want %q", resp.Content, "hi there")
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
	if resp.Usage.CacheReadTokens != 4 {
		t.Errorf("CacheReadTokens = %d, want 4", resp.Usage.CacheReadTokens)
	}
}

// A response with no choices must be an error rather than an empty answer
// silently reaching the user.
func TestComplete_NoChoices(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	})

	if _, err := client.Complete(context.Background(), basicRequest()); err == nil {
		t.Fatal("Complete with no choices = nil error, want error")
	}
}

// An invalid API key must surface the provider's own message, and must come
// back from Complete rather than as an empty success.
func TestComplete_InvalidKey(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "Invalid API Key"},
		})
	})

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Complete with bad key = nil error, want error")
	}
	if !strings.Contains(err.Error(), "Invalid API Key") {
		t.Errorf("error = %v, want it to carry the provider message", err)
	}
}

func TestStream_Success(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}

		// A keep-alive comment and a blank line are interleaved deliberately:
		// real providers send them, and they must not be decoded as content.
		frames := []string{
			`: keep-alive`,
			`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
			``,
			`data: {"choices":[{"delta":{"content":" world"}}]}`,
			`data: [DONE]`,
		}
		for _, f := range frames {
			_, _ = fmt.Fprintf(w, "%s\n", f)
			flusher.Flush()
		}
	})

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	content, doneCount, streamErr := drain(t, chunks)
	if streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}
	if content != "Hello world" {
		t.Errorf("content = %q, want %q", content, "Hello world")
	}
	if doneCount != 1 {
		t.Errorf("Done chunks = %d, want exactly 1", doneCount)
	}
}

// Some providers close the body without sending [DONE]. The stream must still
// terminate with exactly one Done chunk, since the SSE handler in retrieval
// relies on it to close out the response.
func TestStream_EOFWithoutDoneSentinel(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n")
	})

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	content, doneCount, streamErr := drain(t, chunks)
	if streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}
	if content != "partial" {
		t.Errorf("content = %q, want %q", content, "partial")
	}
	if doneCount != 1 {
		t.Errorf("Done chunks = %d, want exactly 1", doneCount)
	}
}

// A bad key must be reported by Stream itself, not delivered later on the
// channel — callers branch on the returned error before writing SSE headers.
func TestStream_InvalidKeyReturnsErrorNotChannel(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "Invalid API Key"},
		})
	})

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Stream with bad key = nil error, want error")
	}
	if chunks != nil {
		t.Error("Stream returned a channel alongside an error")
	}
}

func TestListModels(t *testing.T) {
	client := newTestClient(t, "/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "llama-3.3-70b"},
				// Gemini namespaces IDs like this; the prefix must be stripped
				// so the ID is usable as a request model.
				{"id": "models/gemini-2.0-flash"},
				{"id": ""},
			},
		})
	})

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}

	want := []string{"llama-3.3-70b", "gemini-2.0-flash"}
	if len(models) != len(want) {
		t.Fatalf("got %d models, want %d: %+v", len(models), len(want), models)
	}
	for i, id := range want {
		if models[i].ID != id {
			t.Errorf("models[%d].ID = %q, want %q", i, models[i].ID, id)
		}
	}
}

// The client must satisfy the interfaces the rest of the codebase depends on.
var (
	_ llm.Provider    = (*openaicompat.Client)(nil)
	_ llm.ModelLister = (*openaicompat.Client)(nil)
)
