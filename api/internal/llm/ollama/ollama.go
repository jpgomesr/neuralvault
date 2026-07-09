// Package ollama implements llm.Provider backed by an Ollama server.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/llm/types"
)

// Client is an llm.Provider backed by an Ollama server's /api/chat endpoint.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	defaultModel string
}

// New creates a Client and verifies the configured completion model is
// available on the Ollama server before returning, so callers fail fast
// instead of discovering a missing model on the first real request.
//
// Unlike the embedding client, httpClient has no fixed Timeout: a chat
// completion or stream has no bounded expected duration, and
// http.Client.Timeout covers the entire response body read, which would
// truncate a long-running stream. Callers control duration via ctx instead.
func New(ctx context.Context, cfg *config.Config) (*Client, error) {
	c := &Client{
		httpClient:   &http.Client{},
		baseURL:      cfg.Ollama.URL,
		defaultModel: cfg.Ollama.CompletionModel,
	}

	if err := c.ensureModelAvailable(ctx); err != nil {
		return nil, err
	}

	slog.Info("ollama llm provider connected", "url", c.baseURL, "model", c.defaultModel)
	return c, nil
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ensureModelAvailable confirms the configured completion model has already
// been pulled on the Ollama server, matching either the bare model name or
// its default ":latest"-suffixed tag.
func (c *Client) ensureModelAvailable(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("building ollama tags request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("checking ollama availability: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama tags check: unexpected status %d", resp.StatusCode)
	}

	var tags tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return fmt.Errorf("decoding ollama tags response: %w", err)
	}

	for _, m := range tags.Models {
		if m.Name == c.defaultModel || strings.HasPrefix(m.Name, c.defaultModel+":") {
			return nil
		}
	}

	return fmt.Errorf("ollama model %q not found; pull it with `ollama pull %s`", c.defaultModel, c.defaultModel)
}

// model resolves the model to use for req: the per-request override if set,
// otherwise the client's configured default.
func (c *Client) model(req types.CompletionRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return c.defaultModel
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature float32 `json:"temperature"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  chatOptions   `json:"options"`
}

func toChatMessages(messages []types.Message) []chatMessage {
	out := make([]chatMessage, len(messages))
	for i, m := range messages {
		out[i] = chatMessage{Role: string(m.Role), Content: m.Content}
	}
	return out
}

func newChatRequest(req types.CompletionRequest, model string, stream bool) chatRequest {
	options := chatOptions{Temperature: req.Temperature}
	if req.MaxTokens > 0 {
		options.NumPredict = req.MaxTokens
	}
	return chatRequest{
		Model:    model,
		Messages: toChatMessages(req.Messages),
		Stream:   stream,
		Options:  options,
	}
}

type chatResponse struct {
	Model           string      `json:"model"`
	Message         chatMessage `json:"message"`
	Done            bool        `json:"done"`
	PromptEvalCount int         `json:"prompt_eval_count"`
	EvalCount       int         `json:"eval_count"`
}

// decodeErrorBody reads a non-200 response body's optional {"error": "..."}
// field and returns a descriptive error either way.
func decodeErrorBody(resp *http.Response) error {
	var apiErr struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&apiErr)
	if apiErr.Error != "" {
		return fmt.Errorf("ollama chat request failed (status %d): %s", resp.StatusCode, apiErr.Error)
	}
	return fmt.Errorf("ollama chat request failed: unexpected status %d", resp.StatusCode)
}

// Complete sends a blocking chat completion request and returns the full response.
func (c *Client) Complete(ctx context.Context, req types.CompletionRequest) (types.CompletionResponse, error) {
	model := c.model(req)
	start := time.Now()

	body, err := json.Marshal(newChatRequest(req, model, false))
	if err != nil {
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "model", model)
		return types.CompletionResponse{}, fmt.Errorf("marshaling ollama chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "model", model)
		return types.CompletionResponse{}, fmt.Errorf("building ollama chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "model", model)
		return types.CompletionResponse{}, fmt.Errorf("calling ollama chat endpoint: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		err := decodeErrorBody(resp)
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "model", model)
		return types.CompletionResponse{}, err
	}

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "model", model)
		return types.CompletionResponse{}, fmt.Errorf("decoding ollama chat response: %w", err)
	}

	slog.InfoContext(ctx, "llm completion",
		"model", model,
		"duration_ms", time.Since(start).Milliseconds(),
		"prompt_tokens", out.PromptEvalCount,
		"completion_tokens", out.EvalCount,
	)

	return types.CompletionResponse{
		Content: out.Message.Content,
		Model:   out.Model,
		// CacheReadTokens/CacheCreationTokens are left zero: Ollama's API has no
		// cache concept.
		Usage: types.Usage{
			PromptTokens:     out.PromptEvalCount,
			CompletionTokens: out.EvalCount,
			TotalTokens:      out.PromptEvalCount + out.EvalCount,
		},
	}, nil
}

// Stream sends a streaming chat completion request and returns a channel that
// emits incremental chunks as the model generates them. The initial request
// is issued and status-checked synchronously so a bad model or connection
// failure is returned as an error from Stream itself; the channel is fed by
// a background goroutine reading Ollama's newline-delimited JSON stream.
func (c *Client) Stream(ctx context.Context, req types.CompletionRequest) (<-chan types.StreamChunk, error) {
	model := c.model(req)
	body, err := json.Marshal(newChatRequest(req, model, true))
	if err != nil {
		slog.ErrorContext(ctx, "llm stream failed", "err", err, "model", model)
		return nil, fmt.Errorf("marshaling ollama chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		slog.ErrorContext(ctx, "llm stream failed", "err", err, "model", model)
		return nil, fmt.Errorf("building ollama chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		slog.ErrorContext(ctx, "llm stream failed", "err", err, "model", model)
		return nil, fmt.Errorf("calling ollama chat endpoint: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close() //nolint:errcheck
		err := decodeErrorBody(resp)
		slog.ErrorContext(ctx, "llm stream failed", "err", err, "model", model)
		return nil, err
	}

	slog.InfoContext(ctx, "llm stream started", "model", model)

	chunks := make(chan types.StreamChunk)
	go streamChunks(ctx, resp, chunks)
	return chunks, nil
}

// streamChunks reads resp.Body line-by-line — Ollama sends one JSON object
// per line while streaming — decoding each into a StreamChunk until the
// Done chunk is sent or the body read fails (including ctx cancellation,
// which unblocks the read via the request's context).
func streamChunks(ctx context.Context, resp *http.Response, chunks chan<- types.StreamChunk) {
	defer resp.Body.Close() //nolint:errcheck
	defer close(chunks)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var out chatResponse
		if err := json.Unmarshal(line, &out); err != nil {
			err = fmt.Errorf("decoding ollama stream chunk: %w", err)
			slog.ErrorContext(ctx, "llm stream failed", "err", err)
			chunks <- types.StreamChunk{Error: err, Done: true}
			return
		}

		chunks <- types.StreamChunk{Content: out.Message.Content, Done: out.Done}
		if out.Done {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		err = fmt.Errorf("reading ollama stream: %w", err)
		slog.ErrorContext(ctx, "llm stream failed", "err", err)
		chunks <- types.StreamChunk{Error: err, Done: true}
	}
}
