// Package anthropic implements llm.Provider against Anthropic's native
// Messages API.
//
// Anthropic also offers an OpenAI-compatible shim, which would have let it
// reuse the openaicompat client. The native API is used instead because only it
// reports prompt-cache token counts, which llm/types.Usage already models
// (CacheReadTokens / CacheCreationTokens) and which are otherwise always zero.
package anthropic

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

	"github.com/jpgomesr/NeuralVault/internal/llm/types"
)

// apiVersion pins the Messages API version. Anthropic requires this header on
// every request and uses it to keep responses stable across API changes.
const apiVersion = "2023-06-01"

// defaultMaxTokens is sent when a caller does not set MaxTokens. The Messages
// API requires max_tokens on every request — unlike the OpenAI schema, where it
// is optional — so there is no way to simply omit it.
const defaultMaxTokens = 4096

// Client is an llm.Provider backed by Anthropic's /v1/messages endpoint.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	defaultModel string
}

// New creates a Client.
//
// As with the other LLM clients, httpClient has no Timeout: it would cover the
// whole body read and truncate a long stream. Duration is bounded by ctx.
func New(baseURL, apiKey, defaultModel string) *Client {
	return &Client{
		httpClient:   &http.Client{},
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		apiKey:       apiKey,
		defaultModel: defaultModel,
	}
}

// model resolves the model to use for req: the per-request override if set,
// otherwise the client's configured default.
func (c *Client) model(req types.CompletionRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return c.defaultModel
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// messagesRequest mirrors POST /v1/messages. The system prompt is a top-level
// field here, not a message with role "system" as in the OpenAI schema — see
// splitSystem.
type messagesRequest struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	// Temperature is a pointer so an unset value is omitted rather than sent as
	// 0, which Anthropic would read as fully deterministic sampling.
	Temperature *float32 `json:"temperature,omitempty"`
	Stream      bool     `json:"stream,omitempty"`
}

type usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type messagesResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage usage `json:"usage"`
}

// splitSystem separates the system prompt from the conversation turns.
//
// Anthropic takes the system prompt as a top-level `system` field and rejects
// a message with role "system", so the RoleSystem entries that retrieval's
// buildMessages produces must be lifted out. Multiple system messages are
// joined, preserving order.
func splitSystem(messages []types.Message) (system string, turns []message) {
	var systemParts []string
	turns = make([]message, 0, len(messages))

	for _, m := range messages {
		if m.Role == types.RoleSystem {
			systemParts = append(systemParts, m.Content)
			continue
		}
		turns = append(turns, message{Role: string(m.Role), Content: m.Content})
	}

	return strings.Join(systemParts, "\n\n"), turns
}

func newMessagesRequest(req types.CompletionRequest, model string, stream bool) messagesRequest {
	system, turns := splitSystem(req.Messages)

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	out := messagesRequest{
		Model:     model,
		Messages:  turns,
		MaxTokens: maxTokens,
		System:    system,
		Stream:    stream,
	}

	if req.Temperature > 0 {
		out.Temperature = &req.Temperature
	}

	return out
}

// newRequest builds an authenticated request. Anthropic authenticates with the
// x-api-key header, not an Authorization bearer token.
func (c *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	var req *http.Request
	var err error
	if reader != nil {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("building anthropic request: %w", err)
	}

	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// decodeErrorBody turns a non-200 response into an error carrying Anthropic's
// own message when present.
func decodeErrorBody(resp *http.Response) error {
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&apiErr)

	if apiErr.Error.Message != "" {
		return fmt.Errorf("anthropic request failed (status %d): %s", resp.StatusCode, apiErr.Error.Message)
	}
	return fmt.Errorf("anthropic request failed: unexpected status %d", resp.StatusCode)
}

// Complete sends a blocking message request and returns the full response.
func (c *Client) Complete(ctx context.Context, req types.CompletionRequest) (types.CompletionResponse, error) {
	model := c.model(req)
	start := time.Now()

	body, err := json.Marshal(newMessagesRequest(req, model, false))
	if err != nil {
		return types.CompletionResponse{}, fmt.Errorf("marshaling anthropic messages request: %w", err)
	}

	httpReq, err := c.newRequest(ctx, http.MethodPost, "/v1/messages", body)
	if err != nil {
		return types.CompletionResponse{}, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "provider", "anthropic", "model", model)
		return types.CompletionResponse{}, fmt.Errorf("calling anthropic messages endpoint: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		err := decodeErrorBody(resp)
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "provider", "anthropic", "model", model)
		return types.CompletionResponse{}, err
	}

	var out messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "provider", "anthropic", "model", model)
		return types.CompletionResponse{}, fmt.Errorf("decoding anthropic messages response: %w", err)
	}

	// The content array can interleave block types (text, tool_use, …); only
	// text blocks carry the answer.
	var content strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}

	slog.InfoContext(ctx, "llm completion",
		"provider", "anthropic",
		"model", model,
		"duration_ms", time.Since(start).Milliseconds(),
		"prompt_tokens", out.Usage.InputTokens,
		"completion_tokens", out.Usage.OutputTokens,
		"cache_read_tokens", out.Usage.CacheReadInputTokens,
	)

	return types.CompletionResponse{
		Content: content.String(),
		Model:   out.Model,
		Usage:   toUsage(out.Usage),
	}, nil
}

// toUsage maps Anthropic's token counts onto the shared Usage type. Anthropic
// reports input and output separately with no total, so TotalTokens is derived.
// Cache tokens are counted separately from input_tokens by the API and are not
// added into the total here, matching how Anthropic bills and reports them.
func toUsage(u usage) types.Usage {
	return types.Usage{
		PromptTokens:        u.InputTokens,
		CompletionTokens:    u.OutputTokens,
		TotalTokens:         u.InputTokens + u.OutputTokens,
		CacheReadTokens:     u.CacheReadInputTokens,
		CacheCreationTokens: u.CacheCreationInputTokens,
	}
}

// Stream sends a streaming message request and returns a channel that emits
// incremental chunks. As with the other providers, the initial request and its
// status check are synchronous so a bad key or unknown model is returned as an
// error from Stream itself.
func (c *Client) Stream(ctx context.Context, req types.CompletionRequest) (<-chan types.StreamChunk, error) {
	model := c.model(req)

	body, err := json.Marshal(newMessagesRequest(req, model, true))
	if err != nil {
		return nil, fmt.Errorf("marshaling anthropic messages request: %w", err)
	}

	httpReq, err := c.newRequest(ctx, http.MethodPost, "/v1/messages", body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		slog.ErrorContext(ctx, "llm stream failed", "err", err, "provider", "anthropic", "model", model)
		return nil, fmt.Errorf("calling anthropic messages endpoint: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close() //nolint:errcheck
		err := decodeErrorBody(resp)
		slog.ErrorContext(ctx, "llm stream failed", "err", err, "provider", "anthropic", "model", model)
		return nil, err
	}

	slog.InfoContext(ctx, "llm stream started", "provider", "anthropic", "model", model)

	chunks := make(chan types.StreamChunk)
	go streamChunks(ctx, resp, chunks)
	return chunks, nil
}

// streamEvent is one `data:` frame of a Messages stream. Anthropic's stream is
// typed: content_block_delta carries the text, message_stop terminates it, and
// error reports a mid-stream failure. Other event types (message_start,
// ping, …) are ignored.
type streamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Text string `json:"text"`
	} `json:"delta"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// streamChunks reads Anthropic's SSE body, emitting one StreamChunk per text
// delta. Exactly one Done chunk is emitted before the channel closes, whether
// the stream ends on message_stop, on a clean EOF, or on an error.
func streamChunks(ctx context.Context, resp *http.Response, chunks chan<- types.StreamChunk) {
	defer resp.Body.Close() //nolint:errcheck
	defer close(chunks)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Anthropic sends a redundant `event:` line before each `data:` line;
		// the type is already inside the JSON payload, so only data is read.
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		var event streamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			err = fmt.Errorf("decoding anthropic stream chunk: %w", err)
			slog.ErrorContext(ctx, "llm stream failed", "err", err, "provider", "anthropic")
			chunks <- types.StreamChunk{Error: err, Done: true}
			return
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta.Text != "" {
				chunks <- types.StreamChunk{Content: event.Delta.Text}
			}
		case "message_stop":
			chunks <- types.StreamChunk{Done: true}
			return
		case "error":
			err := fmt.Errorf("anthropic stream error: %s", event.Error.Message)
			slog.ErrorContext(ctx, "llm stream failed", "err", err, "provider", "anthropic")
			chunks <- types.StreamChunk{Error: err, Done: true}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		err = fmt.Errorf("reading anthropic stream: %w", err)
		slog.ErrorContext(ctx, "llm stream failed", "err", err, "provider", "anthropic")
		chunks <- types.StreamChunk{Error: err, Done: true}
		return
	}

	chunks <- types.StreamChunk{Done: true}
}

type modelsResponse struct {
	Data []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"data"`
}

// ListModels returns the models the API key can access, via GET /v1/models.
// It doubles as the credential probe: an invalid key fails here.
func (c *Client) ListModels(ctx context.Context) ([]types.ModelInfo, error) {
	httpReq, err := c.newRequest(ctx, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling anthropic models endpoint: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, decodeErrorBody(resp)
	}

	var out modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding anthropic models response: %w", err)
	}

	models := make([]types.ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID == "" {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		models = append(models, types.ModelInfo{ID: m.ID, Name: name})
	}

	return models, nil
}
