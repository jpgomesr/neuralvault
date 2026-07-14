package anthropic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/llm/anthropic"
)

func newTestClient(t *testing.T, path string, handler http.HandlerFunc) *anthropic.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return anthropic.New(srv.URL, "test-key", "claude-sonnet-5")
}

func basicRequest() llm.CompletionRequest {
	return llm.CompletionRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	}
}

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
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", got)
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version header is required but was not sent")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "claude-sonnet-5",
			// A tool_use block is interleaved to prove only text blocks are
			// concatenated into the answer.
			"content": []map[string]any{
				{"type": "text", "text": "hi "},
				{"type": "tool_use", "text": "IGNORED"},
				{"type": "text", "text": "there"},
			},
			"usage": map[string]int{
				"input_tokens":                10,
				"output_tokens":               5,
				"cache_read_input_tokens":     3,
				"cache_creation_input_tokens": 2,
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
	// The cache counts are the whole reason this provider uses the native API
	// instead of the OpenAI-compatible shim.
	if resp.Usage.CacheReadTokens != 3 {
		t.Errorf("CacheReadTokens = %d, want 3", resp.Usage.CacheReadTokens)
	}
	if resp.Usage.CacheCreationTokens != 2 {
		t.Errorf("CacheCreationTokens = %d, want 2", resp.Usage.CacheCreationTokens)
	}
}

// Anthropic rejects a message with role "system": the system prompt is a
// top-level field. retrieval's buildMessages emits one, so it must be lifted
// out of the messages array.
func TestComplete_SystemPromptIsLiftedOutOfMessages(t *testing.T) {
	var got struct {
		System []struct {
			Type         string `json:"type"`
			Text         string `json:"text"`
			CacheControl *struct {
				Type string `json:"type"`
			} `json:"cache_control"`
		} `json:"system"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		MaxTokens int `json:"max_tokens"`
	}

	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	})

	_, err := client.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "be terse"},
			{Role: llm.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if len(got.System) != 1 || got.System[0].Text != "be terse" {
		t.Fatalf("system = %+v, want a single block with text %q", got.System, "be terse")
	}
	// The system prompt is identical on every request NeuralVault ever sends,
	// so it must always be marked cacheable — this is the whole reason it's a
	// content-block array instead of a plain string.
	if got.System[0].CacheControl == nil || got.System[0].CacheControl.Type != "ephemeral" {
		t.Errorf("cache_control = %+v, want {type: ephemeral}", got.System[0].CacheControl)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" {
		t.Errorf("messages = %+v, want only the user turn", got.Messages)
	}
	// max_tokens is mandatory on the Messages API, so a request that omits it
	// must still send a default.
	if got.MaxTokens <= 0 {
		t.Errorf("max_tokens = %d, want a positive default", got.MaxTokens)
	}
}

// No system prompt means nothing to cache: the field must be omitted rather
// than sent as an empty array, matching the previous plain-string contract.
func TestComplete_NoSystemPromptOmitsSystemField(t *testing.T) {
	raw := map[string]any{}

	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	})

	_, err := client.Complete(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if _, ok := raw["system"]; ok {
		t.Errorf(`request body has a "system" key, want it omitted: %v`, raw)
	}
}

func TestComplete_InvalidKey(t *testing.T) {
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "invalid x-api-key"},
		})
	})

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Complete with bad key = nil error, want error")
	}
	if !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Errorf("error = %v, want it to carry the provider message", err)
	}
}

func TestStream_Success(t *testing.T) {
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}

		// Anthropic prefixes each data line with a redundant event line, and
		// sends event types that carry no text; both must be skipped.
		frames := []string{
			`event: message_start`,
			`data: {"type":"message_start"}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","delta":{"text":"Hello"}}`,
			``,
			`data: {"type":"ping"}`,
			`data: {"type":"content_block_delta","delta":{"text":" world"}}`,
			`data: {"type":"message_stop"}`,
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

// A mid-stream error event must reach the caller as a StreamChunk.Error, since
// by then the SSE response has already been committed.
func TestStream_MidStreamError(t *testing.T) {
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"part\"}}\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"error\",\"error\":{\"message\":\"overloaded\"}}\n")
	})

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	content, doneCount, streamErr := drain(t, chunks)
	if streamErr == nil {
		t.Fatal("stream error = nil, want the provider's error event")
	}
	if !strings.Contains(streamErr.Error(), "overloaded") {
		t.Errorf("stream error = %v, want it to carry the provider message", streamErr)
	}
	if content != "part" {
		t.Errorf("content = %q, want the partial text emitted before the error", content)
	}
	if doneCount != 1 {
		t.Errorf("Done chunks = %d, want exactly 1", doneCount)
	}
}

func TestStream_InvalidKeyReturnsErrorNotChannel(t *testing.T) {
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "invalid x-api-key"},
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
	client := newTestClient(t, "/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "claude-opus-4-8", "display_name": "Claude Opus 4.8"},
				{"id": "claude-haiku-4-5-20251001"},
			},
		})
	})

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("got %d models, want 2: %+v", len(models), models)
	}
	if models[0].Name != "Claude Opus 4.8" {
		t.Errorf("models[0].Name = %q, want the display name", models[0].Name)
	}
	// With no display name, the ID stands in so the UI always has a label.
	if models[1].Name != "claude-haiku-4-5-20251001" {
		t.Errorf("models[1].Name = %q, want it to fall back to the ID", models[1].Name)
	}
}

var (
	_ llm.Provider    = (*anthropic.Client)(nil)
	_ llm.ModelLister = (*anthropic.Client)(nil)
)
