package openaicompat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// TestComplete_ModelOverride verifies req.Model wins over the client's
// configured default, the per-request model-picker override path.
func TestComplete_ModelOverride(t *testing.T) {
	var got struct {
		Model string `json:"model"`
	}
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "ok"}}},
		})
	})

	req := basicRequest()
	req.Model = "llama-3.1-8b"
	if _, err := client.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.Model != "llama-3.1-8b" {
		t.Errorf("model = %q, want the request override %q", got.Model, "llama-3.1-8b")
	}
}

// TestComplete_ErrorBodyWithNoMessage covers decodeErrorBody's fallback: a
// non-200 response whose body carries no "error.message" still needs to
// surface the HTTP status, not a blank error.
func TestComplete_ErrorBodyWithNoMessage(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Complete = nil error, want an error for a 503 with no body")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %v, want it to mention the status code", err)
	}
}

// TestComplete_MalformedResponseBody covers the JSON-decode failure path.
func TestComplete_MalformedResponseBody(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "not json")
	})

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Complete = nil error, want a decode error")
	}
}

// TestComplete_ConnectionRefused covers the http.Client.Do failure path
// shared by Complete/Stream/ListModels.
func TestComplete_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	client := openaicompat.New("groq", srv.URL, "test-key", "llama-3.3-70b")
	srv.Close()

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Complete against a closed server = nil error, want a connection error")
	}
}

// TestComplete_InvalidBaseURL covers newRequest's own error path (a baseURL
// that fails url.Parse), shared by Complete/Stream/ListModels.
func TestComplete_InvalidBaseURL(t *testing.T) {
	client := openaicompat.New("groq", "://bad-url", "test-key", "llama-3.3-70b")

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Complete with an invalid base URL = nil error, want an error")
	}
}

// TestComplete_MarshalError covers the json.Marshal failure path shared by
// Complete/Stream: +Inf isn't representable in JSON, the one way to make a
// chatRequest built from valid inputs fail to encode.
func TestComplete_MarshalError(t *testing.T) {
	client := openaicompat.New("groq", "http://unused.invalid", "test-key", "llama-3.3-70b")

	req := basicRequest()
	req.Temperature = float32(math.Inf(1))
	_, err := client.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("Complete with a non-finite temperature = nil error, want a marshal error")
	}
}

func TestStream_MarshalError(t *testing.T) {
	client := openaicompat.New("groq", "http://unused.invalid", "test-key", "llama-3.3-70b")

	req := basicRequest()
	req.Temperature = float32(math.Inf(1))
	_, err := client.Stream(context.Background(), req)
	if err == nil {
		t.Fatal("Stream with a non-finite temperature = nil error, want a marshal error")
	}
}

func TestStream_InvalidBaseURL(t *testing.T) {
	client := openaicompat.New("groq", "://bad-url", "test-key", "llama-3.3-70b")

	_, err := client.Stream(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Stream with an invalid base URL = nil error, want an error")
	}
}

func TestStream_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	client := openaicompat.New("groq", srv.URL, "test-key", "llama-3.3-70b")
	srv.Close()

	_, err := client.Stream(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Stream against a closed server = nil error, want a connection error")
	}
}

// TestStream_SkipsCommentLines covers the SSE keep-alive comment branch (a
// line prefixed with ":"), which some providers send to hold the connection
// open — it must be ignored rather than treated as a malformed frame.
func TestStream_SkipsCommentLines(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}
		_, _ = fmt.Fprint(w, ": keep-alive\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	})

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	content, doneCount, streamErr := drain(t, chunks)
	if streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}
	if content != "hi" {
		t.Errorf("content = %q, want %q", content, "hi")
	}
	if doneCount != 1 {
		t.Errorf("Done chunks = %d, want exactly 1", doneCount)
	}
}

// TestStream_SkipsUnrecognizedSSEFields covers a non-blank, non-comment line
// that isn't a "data:" frame either (e.g. an "id:" or "retry:" field some
// providers send) — it must be ignored, not treated as a malformed frame.
func TestStream_SkipsUnrecognizedSSEFields(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}
		_, _ = fmt.Fprint(w, "id: 42\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	})

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	content, doneCount, streamErr := drain(t, chunks)
	if streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}
	if content != "hi" {
		t.Errorf("content = %q, want %q", content, "hi")
	}
	if doneCount != 1 {
		t.Errorf("Done chunks = %d, want exactly 1", doneCount)
	}
}

// TestStream_SkipsFramesWithNoChoices covers a frame whose choices array is
// empty — some providers send one before the first content delta.
func TestStream_SkipsFramesWithNoChoices(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	})

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	content, doneCount, streamErr := drain(t, chunks)
	if streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}
	if content != "hi" {
		t.Errorf("content = %q, want %q", content, "hi")
	}
	if doneCount != 1 {
		t.Errorf("Done chunks = %d, want exactly 1", doneCount)
	}
}

// TestStream_MalformedFrame covers streamChunks' JSON-decode failure path.
func TestStream_MalformedFrame(t *testing.T) {
	client := newTestClient(t, "/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {not valid json\n")
	})

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	_, doneCount, streamErr := drain(t, chunks)
	if streamErr == nil {
		t.Fatal("stream error = nil, want a decode error for a malformed frame")
	}
	if doneCount != 1 {
		t.Errorf("Done chunks = %d, want exactly 1", doneCount)
	}
}

// TestStream_TruncatedBody covers scanner.Err(): a response that declares
// more bytes (Content-Length) than it actually sends before closing forces a
// genuine I/O error, distinct from a clean EOF
// (TestStream_EOFWithoutDoneSentinel).
func TestStream_TruncatedBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		frame := "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"
		w.Header().Set("Content-Length", strconv.Itoa(len(frame)+1000))
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, frame)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		_ = conn.Close()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := openaicompat.New("groq", srv.URL, "test-key", "llama-3.3-70b")

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	_, doneCount, streamErr := drain(t, chunks)
	if streamErr == nil {
		t.Fatal("stream error = nil, want a read error for a truncated body")
	}
	if doneCount != 1 {
		t.Errorf("Done chunks = %d, want exactly 1", doneCount)
	}
}

func TestListModels_InvalidBaseURL(t *testing.T) {
	client := openaicompat.New("groq", "://bad-url", "test-key", "llama-3.3-70b")

	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("ListModels with an invalid base URL = nil error, want an error")
	}
}

func TestListModels_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	client := openaicompat.New("groq", srv.URL, "test-key", "llama-3.3-70b")
	srv.Close()

	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("ListModels against a closed server = nil error, want a connection error")
	}
}

func TestListModels_ErrorStatus(t *testing.T) {
	client := newTestClient(t, "/models", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "Invalid API Key"},
		})
	})

	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("ListModels with bad key = nil error, want error")
	}
	if !strings.Contains(err.Error(), "Invalid API Key") {
		t.Errorf("error = %v, want it to carry the provider message", err)
	}
}

func TestListModels_MalformedResponseBody(t *testing.T) {
	client := newTestClient(t, "/models", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "not json")
	})

	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("ListModels = nil error, want a decode error")
	}
}

// The client must satisfy the interfaces the rest of the codebase depends on.
var (
	_ llm.Provider    = (*openaicompat.Client)(nil)
	_ llm.ModelLister = (*openaicompat.Client)(nil)
)
