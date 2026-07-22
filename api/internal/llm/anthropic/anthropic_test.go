package anthropic_test

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

	"github.com/jpgomesr/neuralvault/api/internal/llm"
	"github.com/jpgomesr/neuralvault/api/internal/llm/anthropic"
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

	models, err := client.ListModels(context.Background(), llm.PurposeAny)
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

// TestComplete_ModelOverride verifies req.Model wins over the client's
// configured default, the per-request model-picker override path.
func TestComplete_ModelOverride(t *testing.T) {
	var got struct {
		Model string `json:"model"`
	}
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	})

	req := basicRequest()
	req.Model = "claude-haiku-4-5"
	if _, err := client.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.Model != "claude-haiku-4-5" {
		t.Errorf("model = %q, want the request override %q", got.Model, "claude-haiku-4-5")
	}
}

// TestComplete_TemperatureIsForwarded verifies a positive Temperature is sent;
// newMessagesRequest omits it (rather than sending 0) when unset, so Anthropic
// doesn't read a caller's silence as "fully deterministic sampling".
func TestComplete_TemperatureIsForwarded(t *testing.T) {
	raw := map[string]any{}
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	})

	req := basicRequest()
	req.Temperature = 0.7
	if _, err := client.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got, ok := raw["temperature"].(float64); !ok || got != 0.7 {
		t.Errorf("temperature = %v, want 0.7", raw["temperature"])
	}
}

// TestComplete_ErrorBodyWithNoMessage covers decodeErrorBody's fallback: a
// non-200 response whose body carries no "error.message" still needs to
// surface the HTTP status, not a blank error.
func TestComplete_ErrorBodyWithNoMessage(t *testing.T) {
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
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

// TestComplete_MalformedResponseBody covers the JSON-decode failure path: a
// 200 response whose body isn't valid JSON must still surface as an error,
// not a panic or a silently empty answer.
func TestComplete_MalformedResponseBody(t *testing.T) {
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "not json")
	})

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Complete = nil error, want a decode error")
	}
}

// TestComplete_ConnectionRefused covers the http.Client.Do failure path
// shared by Complete/Stream/ListModels: a client pointed at a server that has
// already stopped accepting connections must return an error, not panic.
func TestComplete_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	client := anthropic.New(srv.URL, "test-key", "claude-sonnet-5")
	srv.Close() // stop accepting before the client ever sends anything

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Complete against a closed server = nil error, want a connection error")
	}
}

// TestComplete_InvalidBaseURL covers newRequest's own error path (a baseURL
// that fails url.Parse), shared by Complete/Stream/ListModels.
func TestComplete_InvalidBaseURL(t *testing.T) {
	client := anthropic.New("://bad-url", "test-key", "claude-sonnet-5")

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Complete with an invalid base URL = nil error, want an error")
	}
}

func TestStream_InvalidBaseURL(t *testing.T) {
	client := anthropic.New("://bad-url", "test-key", "claude-sonnet-5")

	_, err := client.Stream(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Stream with an invalid base URL = nil error, want an error")
	}
}

func TestStream_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	client := anthropic.New(srv.URL, "test-key", "claude-sonnet-5")
	srv.Close()

	_, err := client.Stream(context.Background(), basicRequest())
	if err == nil {
		t.Fatal("Stream against a closed server = nil error, want a connection error")
	}
}

// TestStream_EndsCleanlyWithoutMessageStop covers streamChunks' fallthrough:
// a stream that reaches EOF without a message_stop or error event (e.g. the
// connection just closes) must still emit exactly one Done chunk rather than
// hanging or silently dropping it.
func TestStream_EndsCleanlyWithoutMessageStop(t *testing.T) {
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"partial\"}}\n")
		flusher.Flush()
		// Handler returns without a message_stop or error event — the
		// connection closes and the scanner sees a clean EOF.
	})

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	content, doneCount, streamErr := drain(t, chunks)
	if streamErr != nil {
		t.Fatalf("stream error: %v, want none for a clean EOF", streamErr)
	}
	if content != "partial" {
		t.Errorf("content = %q, want %q", content, "partial")
	}
	if doneCount != 1 {
		t.Errorf("Done chunks = %d, want exactly 1", doneCount)
	}
}

// TestStream_MalformedFrame covers streamChunks' JSON-decode failure path: a
// `data:` line that isn't valid JSON must end the stream with an error, not
// panic or hang.
func TestStream_MalformedFrame(t *testing.T) {
	client := newTestClient(t, "/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
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
// more bytes (Content-Length) than it actually sends before closing forces
// the client's read to fail with a genuine I/O error, distinct from a clean
// EOF (TestStream_EndsCleanlyWithoutMessageStop) — both must still terminate
// the channel with exactly one Done chunk carrying the error.
func TestStream_TruncatedBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		frame := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"partial\"}}\n"
		w.Header().Set("Content-Length", strconv.Itoa(len(frame)+1000))
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, frame)
		// Hijack and close the raw connection immediately: returning normally
		// would let the server pad or error out the declared length itself,
		// masking the truncation from the client.
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
	client := anthropic.New(srv.URL, "test-key", "claude-sonnet-5")

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

// TestComplete_MarshalError covers the json.Marshal failure path shared by
// Complete/Stream: +Inf isn't representable in JSON, the one way to make a
// messagesRequest built from valid inputs fail to encode.
func TestComplete_MarshalError(t *testing.T) {
	client := anthropic.New("http://unused.invalid", "test-key", "claude-sonnet-5")

	req := basicRequest()
	req.Temperature = float32(math.Inf(1))
	_, err := client.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("Complete with a non-finite temperature = nil error, want a marshal error")
	}
}

func TestStream_MarshalError(t *testing.T) {
	client := anthropic.New("http://unused.invalid", "test-key", "claude-sonnet-5")

	req := basicRequest()
	req.Temperature = float32(math.Inf(1))
	_, err := client.Stream(context.Background(), req)
	if err == nil {
		t.Fatal("Stream with a non-finite temperature = nil error, want a marshal error")
	}
}

func TestListModels_InvalidBaseURL(t *testing.T) {
	client := anthropic.New("://bad-url", "test-key", "claude-sonnet-5")

	_, err := client.ListModels(context.Background(), llm.PurposeAny)
	if err == nil {
		t.Fatal("ListModels with an invalid base URL = nil error, want an error")
	}
}

func TestListModels_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	client := anthropic.New(srv.URL, "test-key", "claude-sonnet-5")
	srv.Close()

	_, err := client.ListModels(context.Background(), llm.PurposeAny)
	if err == nil {
		t.Fatal("ListModels against a closed server = nil error, want a connection error")
	}
}

func TestListModels_ErrorStatus(t *testing.T) {
	client := newTestClient(t, "/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "invalid x-api-key"},
		})
	})

	_, err := client.ListModels(context.Background(), llm.PurposeAny)
	if err == nil {
		t.Fatal("ListModels with bad key = nil error, want error")
	}
	if !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Errorf("error = %v, want it to carry the provider message", err)
	}
}

func TestListModels_MalformedResponseBody(t *testing.T) {
	client := newTestClient(t, "/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "not json")
	})

	_, err := client.ListModels(context.Background(), llm.PurposeAny)
	if err == nil {
		t.Fatal("ListModels = nil error, want a decode error")
	}
}

// TestListModels_SkipsEntriesWithNoID covers the defensive skip in
// ListModels' mapping loop: a catalog entry with no id can't be selected
// (there is nothing to send back as CompletionRequest.Model), so it must be
// dropped instead of surfaced as an unusable, blank-ID choice.
func TestListModels_SkipsEntriesWithNoID(t *testing.T) {
	client := newTestClient(t, "/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "", "display_name": "should be skipped"},
				{"id": "claude-sonnet-5", "display_name": "Claude Sonnet 5"},
			},
		})
	})

	models, err := client.ListModels(context.Background(), llm.PurposeAny)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "claude-sonnet-5" {
		t.Fatalf("models = %+v, want only the entry with a non-empty id", models)
	}
}

var (
	_ llm.Provider    = (*anthropic.Client)(nil)
	_ llm.ModelLister = (*anthropic.Client)(nil)
)
