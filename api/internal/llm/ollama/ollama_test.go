package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/llm/ollama"
)

// validTagsHandler serves a /api/tags response listing "llama3:latest",
// matching the default tag Ollama assigns when a model is pulled without an
// explicit version.
func validTagsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"models": []map[string]string{{"name": "llama3:latest"}},
	})
}

// newTestClient builds an httptest server serving a valid /api/tags response
// and the given chat handler at /api/chat, then constructs a real
// ollama.Client against it via ollama.New.
func newTestClient(t *testing.T, chatHandler http.HandlerFunc) *ollama.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", validTagsHandler)
	mux.HandleFunc("/api/chat", chatHandler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, CompletionModel: "llama3"}}
	client, err := ollama.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}
	return client
}

func basicRequest() llm.CompletionRequest {
	return llm.CompletionRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	}
}

func TestComplete_Success(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":             "llama3",
			"message":           map[string]string{"role": "assistant", "content": "hi there"},
			"done":              true,
			"prompt_eval_count": 5,
			"eval_count":        3,
		})
	})

	resp, err := client.Complete(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hi there" {
		t.Fatalf("expected content %q, got %q", "hi there", resp.Content)
	}
	if resp.Model != "llama3" {
		t.Fatalf("expected model %q, got %q", "llama3", resp.Model)
	}
	if resp.Usage.PromptTokens != 5 || resp.Usage.CompletionTokens != 3 || resp.Usage.TotalTokens != 8 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestComplete_RequestBody(t *testing.T) {
	var gotBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"role": "assistant", "content": "ok"},
			"done":    true,
		})
	})

	req := llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "be concise"},
			{Role: llm.RoleUser, Content: "hello world"},
		},
		Temperature: 0.5,
		MaxTokens:   128,
	}
	if _, err := client.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotBody["model"] != "llama3" {
		t.Fatalf("expected model %q, got %v", "llama3", gotBody["model"])
	}
	if gotBody["stream"] != false {
		t.Fatalf("expected stream=false, got %v", gotBody["stream"])
	}
	messages, ok := gotBody["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %v", gotBody["messages"])
	}
	first := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "be concise" {
		t.Fatalf("unexpected first message: %v", first)
	}
	options, ok := gotBody["options"].(map[string]any)
	if !ok {
		t.Fatalf("expected options object, got %v", gotBody["options"])
	}
	if options["temperature"] != 0.5 {
		t.Fatalf("expected temperature 0.5, got %v", options["temperature"])
	}
	if options["num_predict"] != float64(128) {
		t.Fatalf("expected num_predict 128, got %v", options["num_predict"])
	}
}

func TestComplete_MaxTokensZeroOmitsNumPredict(t *testing.T) {
	var gotBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"role": "assistant", "content": "ok"},
			"done":    true,
		})
	})

	if _, err := client.Complete(context.Background(), basicRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	options := gotBody["options"].(map[string]any)
	if _, present := options["num_predict"]; present {
		t.Fatalf("expected num_predict to be omitted when MaxTokens is 0, got %v", options)
	}
}

func TestComplete_ModelOverride(t *testing.T) {
	var gotModel string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "llama3.1",
			"message": map[string]string{"role": "assistant", "content": "ok"},
			"done":    true,
		})
	})

	req := basicRequest()
	req.Model = "llama3.1"
	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotModel != "llama3.1" {
		t.Fatalf("expected request model %q, got %q", "llama3.1", gotModel)
	}
	if resp.Model != "llama3.1" {
		t.Fatalf("expected response model %q, got %q", "llama3.1", resp.Model)
	}
}

func TestComplete_NonOKStatus_WithErrorBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "model not found"})
	})

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil || !strings.Contains(err.Error(), "model not found") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error mentioning status 500 and %q, got: %v", "model not found", err)
	}
}

func TestComplete_NonOKStatus_NoBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected error mentioning status 503, got: %v", err)
	}
}

func TestComplete_MalformedJSON(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	})

	_, err := client.Complete(context.Background(), basicRequest())
	if err == nil || !strings.Contains(err.Error(), "decoding ollama chat response") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}

func TestComplete_NetworkError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", validTagsHandler)
	srv := httptest.NewServer(mux)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, CompletionModel: "llama3"}}
	client, err := ollama.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ollama.New: %v", err)
	}
	srv.Close() // force connection errors on subsequent requests

	_, err = client.Complete(context.Background(), basicRequest())
	if err == nil || !strings.Contains(err.Error(), "calling ollama chat endpoint") {
		t.Fatalf("expected network error, got: %v", err)
	}
}

func TestComplete_ContextCanceled(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"role": "assistant", "content": "ok"},
			"done":    true,
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Complete(ctx, basicRequest())
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context.Canceled, got: %v", err)
	}
}

// ndjsonHandler writes each line as a separate chunk, flushing after every
// write, mimicking Ollama's newline-delimited streaming response.
func ndjsonHandler(lines ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, line := range lines {
			fmt.Fprintln(w, line)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func TestStream_Success(t *testing.T) {
	client := newTestClient(t, ndjsonHandler(
		`{"model":"llama3","message":{"role":"assistant","content":"hel"},"done":false}`,
		`{"model":"llama3","message":{"role":"assistant","content":"lo"},"done":false}`,
		`{"model":"llama3","message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":2,"eval_count":2}`,
	))

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var content strings.Builder
	var sawDone bool
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Error)
		}
		content.WriteString(chunk.Content)
		if chunk.Done {
			sawDone = true
		}
	}

	if !sawDone {
		t.Fatal("expected a final chunk with Done == true")
	}
	if content.String() != "hello" {
		t.Fatalf("expected accumulated content %q, got %q", "hello", content.String())
	}
}

func TestStream_ModelOverride(t *testing.T) {
	var gotModel string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		if body["stream"] != true {
			t.Errorf("expected stream=true, got %v", body["stream"])
		}
		ndjsonHandler(`{"message":{"role":"assistant","content":"ok"},"done":true}`)(w, r)
	})

	req := basicRequest()
	req.Model = "llama3.1"
	chunks, err := client.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range chunks {
	}
	if gotModel != "llama3.1" {
		t.Fatalf("expected request model %q, got %q", "llama3.1", gotModel)
	}
}

func TestStream_MidStreamMalformedLine(t *testing.T) {
	client := newTestClient(t, ndjsonHandler(
		`{"message":{"role":"assistant","content":"hel"},"done":false}`,
		`not json`,
	))

	chunks, err := client.Stream(context.Background(), basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var lastChunk struct {
		content string
		err     error
		done    bool
	}
	for chunk := range chunks {
		lastChunk.content = chunk.Content
		lastChunk.err = chunk.Error
		lastChunk.done = chunk.Done
	}

	if lastChunk.err == nil || !strings.Contains(lastChunk.err.Error(), "decoding ollama stream chunk") {
		t.Fatalf("expected decode error on final chunk, got: %v", lastChunk.err)
	}
	if !lastChunk.done {
		t.Fatal("expected the error chunk to have Done == true")
	}
}

func TestStream_NonOKStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "model not found"})
	})

	_, err := client.Stream(context.Background(), basicRequest())
	if err == nil || !strings.Contains(err.Error(), "model not found") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error mentioning status 500 and %q, got: %v", "model not found", err)
	}
}

func TestStream_ContextCancelStopsGeneration(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"hel"},"done":false}`)
		flusher.Flush()
		select {
		case <-block:
		case <-r.Context().Done():
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	chunks, err := client.Stream(ctx, basicRequest())
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Consume the first chunk, then cancel and expect the channel to close
	// without hanging.
	<-chunks
	cancel()

	select {
	case _, ok := <-chunks:
		if ok {
			// Drain any trailing error chunk emitted before the close.
			for range chunks {
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected channel to close after context cancellation")
	}
}

func TestNew_ModelNotAvailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "nomic-embed-text:latest"}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, CompletionModel: "llama3"}}
	_, err := ollama.New(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "ollama pull llama3") {
		t.Fatalf("expected model-not-found error, got: %v", err)
	}
}

func TestNew_TagsEndpointUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NewServeMux())
	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, CompletionModel: "llama3"}}
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

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, CompletionModel: "llama3"}}
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

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, CompletionModel: "llama3"}}
	if _, err := ollama.New(context.Background(), cfg); err != nil {
		t.Fatalf("expected New to succeed matching %q against tag %q, got: %v",
			"llama3", "llama3:latest", err)
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
			"models": []map[string]string{{"name": "llama3"}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, CompletionModel: "llama3"}}
	if _, err := ollama.New(context.Background(), cfg); err != nil {
		t.Fatalf("expected New to succeed matching %q exactly, got: %v", "llama3", err)
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

	cfg := &config.Config{Ollama: config.Ollama{URL: srv.URL, CompletionModel: "llama3"}}
	_, err := ollama.New(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "decoding ollama tags response") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}
