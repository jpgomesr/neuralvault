// Package openaicompat implements llm.Provider against any OpenAI-compatible
// /chat/completions endpoint.
//
// One client covers several providers because Google AI Studio (Gemini),
// Groq, OpenRouter, GitHub Models and OpenAI itself all speak the same wire
// protocol; only the base URL and the API key differ. See internal/catalog for
// the base URLs.
package openaicompat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jpgomesr/neuralvault/api/internal/catalog"
	"github.com/jpgomesr/neuralvault/api/internal/llm/types"
)

// Client is an llm.Provider backed by an OpenAI-compatible chat API.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	defaultModel string
	// provider names the concrete backend (groq, gemini, …) and is used only
	// to make errors and logs identify who actually failed.
	provider string
}

// New creates a Client.
//
// Unlike the embedding client it sets no http.Client.Timeout: a completion has
// no bounded expected duration, and Timeout covers the entire response body
// read, which would truncate a long stream mid-answer. Duration is bounded by
// the caller's ctx instead — the same reasoning as the Ollama LLM client.
//
// It performs no startup check. A BYOK credential is validated when the user
// saves it (see internal/modelconfig), not on every client construction, and a
// bad key surfaces as an error from the first Complete/Stream call.
func New(provider, baseURL, apiKey, defaultModel string) *Client {
	return &Client{
		httpClient:   &http.Client{},
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		apiKey:       apiKey,
		defaultModel: defaultModel,
		provider:     provider,
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

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float32       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// PromptTokensDetails carries OpenAI's cache-hit count. Providers that do
	// not implement prompt caching simply omit it, leaving it zero.
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage usage `json:"usage"`
}

// streamResponse is one `data:` frame of a streaming completion. Deltas carry
// content incrementally; usage appears only on the final frame, and only for
// providers that report it.
type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func newChatRequest(req types.CompletionRequest, model string, stream bool) chatRequest {
	messages := make([]chatMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = chatMessage{Role: string(m.Role), Content: m.Content}
	}

	return chatRequest{
		Model:       model,
		Messages:    messages,
		Stream:      stream,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
}

// newRequest builds an authenticated JSON request against the provider.
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
		return nil, fmt.Errorf("building %s request: %w", c.provider, err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// decodeErrorBody turns a non-200 response into an error carrying the
// provider's own message when it sends one.
//
// The API key is never included: it is not part of the response body, and
// nothing here echoes the request.
func (c *Client) decodeErrorBody(resp *http.Response) error {
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&apiErr)

	if apiErr.Error.Message != "" {
		return fmt.Errorf("%s request failed (status %d): %s", c.provider, resp.StatusCode, apiErr.Error.Message)
	}
	return fmt.Errorf("%s request failed: unexpected status %d", c.provider, resp.StatusCode)
}

// Complete sends a blocking chat completion request and returns the full response.
func (c *Client) Complete(ctx context.Context, req types.CompletionRequest) (types.CompletionResponse, error) {
	model := c.model(req)
	start := time.Now()

	body, err := json.Marshal(newChatRequest(req, model, false))
	if err != nil {
		return types.CompletionResponse{}, fmt.Errorf("marshaling %s chat request: %w", c.provider, err)
	}

	httpReq, err := c.newRequest(ctx, http.MethodPost, "/chat/completions", body)
	if err != nil {
		return types.CompletionResponse{}, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "provider", c.provider, "model", model)
		return types.CompletionResponse{}, fmt.Errorf("calling %s chat endpoint: %w", c.provider, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		err := c.decodeErrorBody(resp)
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "provider", c.provider, "model", model)
		return types.CompletionResponse{}, err
	}

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		slog.ErrorContext(ctx, "llm completion failed", "err", err, "provider", c.provider, "model", model)
		return types.CompletionResponse{}, fmt.Errorf("decoding %s chat response: %w", c.provider, err)
	}

	if len(out.Choices) == 0 {
		return types.CompletionResponse{}, fmt.Errorf("%s chat response contained no choices", c.provider)
	}

	slog.InfoContext(ctx, "llm completion",
		"provider", c.provider,
		"model", model,
		"duration_ms", time.Since(start).Milliseconds(),
		"prompt_tokens", out.Usage.PromptTokens,
		"completion_tokens", out.Usage.CompletionTokens,
	)

	return types.CompletionResponse{
		Content: out.Choices[0].Message.Content,
		Model:   out.Model,
		Usage: types.Usage{
			PromptTokens:     out.Usage.PromptTokens,
			CompletionTokens: out.Usage.CompletionTokens,
			TotalTokens:      out.Usage.TotalTokens,
			CacheReadTokens:  out.Usage.PromptTokensDetails.CachedTokens,
			// CacheCreationTokens has no equivalent in the OpenAI schema:
			// caching is implicit, so there is no separate cache-write count.
		},
	}, nil
}

// Stream sends a streaming chat completion request and returns a channel that
// emits incremental chunks. The initial request and its status check are
// synchronous, so an invalid API key or unknown model is returned as an error
// from Stream itself rather than arriving later on the channel — matching the
// Ollama provider's contract.
func (c *Client) Stream(ctx context.Context, req types.CompletionRequest) (<-chan types.StreamChunk, error) {
	model := c.model(req)

	body, err := json.Marshal(newChatRequest(req, model, true))
	if err != nil {
		return nil, fmt.Errorf("marshaling %s chat request: %w", c.provider, err)
	}

	httpReq, err := c.newRequest(ctx, http.MethodPost, "/chat/completions", body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		slog.ErrorContext(ctx, "llm stream failed", "err", err, "provider", c.provider, "model", model)
		return nil, fmt.Errorf("calling %s chat endpoint: %w", c.provider, err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close() //nolint:errcheck
		err := c.decodeErrorBody(resp)
		slog.ErrorContext(ctx, "llm stream failed", "err", err, "provider", c.provider, "model", model)
		return nil, err
	}

	slog.InfoContext(ctx, "llm stream started", "provider", c.provider, "model", model)

	chunks := make(chan types.StreamChunk)
	go c.streamChunks(ctx, resp, chunks)
	return chunks, nil
}

// streamDone is the sentinel payload terminating an OpenAI-compatible SSE stream.
const streamDone = "[DONE]"

// streamChunks reads the SSE body, emitting one StreamChunk per content delta.
//
// The stream ends on the `data: [DONE]` sentinel. Some providers close the body
// without sending it, so a clean EOF is also treated as a normal end — either
// way exactly one Done chunk is emitted before the channel closes, which is what
// the SSE handler in retrieval depends on.
func (c *Client) streamChunks(ctx context.Context, resp *http.Response, chunks chan<- types.StreamChunk) {
	defer resp.Body.Close() //nolint:errcheck
	defer close(chunks)

	scanner := bufio.NewScanner(resp.Body)
	// Individual SSE frames can exceed bufio's 64 KiB default line limit when a
	// provider batches a large delta, which would otherwise abort the stream.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Blank lines separate SSE frames; comment lines (":" prefix) are
		// keep-alives some providers send to hold the connection open.
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		payload, found := strings.CutPrefix(line, "data:")
		if !found {
			continue
		}
		payload = strings.TrimSpace(payload)

		if payload == streamDone {
			chunks <- types.StreamChunk{Done: true}
			return
		}

		var out streamResponse
		if err := json.Unmarshal([]byte(payload), &out); err != nil {
			err = fmt.Errorf("decoding %s stream chunk: %w", c.provider, err)
			slog.ErrorContext(ctx, "llm stream failed", "err", err, "provider", c.provider)
			chunks <- types.StreamChunk{Error: err, Done: true}
			return
		}

		if len(out.Choices) == 0 {
			continue
		}

		if content := out.Choices[0].Delta.Content; content != "" {
			chunks <- types.StreamChunk{Content: content}
		}
	}

	if err := scanner.Err(); err != nil {
		err = fmt.Errorf("reading %s stream: %w", c.provider, err)
		slog.ErrorContext(ctx, "llm stream failed", "err", err, "provider", c.provider)
		chunks <- types.StreamChunk{Error: err, Done: true}
		return
	}

	chunks <- types.StreamChunk{Done: true}
}

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// ListModels returns the models the API key can access, via GET /models. It
// doubles as the credential probe: an invalid key fails here.
//
// The OpenAI-compatible /models endpoint reports no per-model capability, so
// purpose cannot be honored through it — every provider but Gemini ignores it
// and returns the full list. Gemini is the one provider whose native API
// (distinct from the OpenAI-compatible endpoint this client otherwise talks
// to) reports which models support embedContent vs generateContent, so a
// requested purpose routes there instead when set.
func (c *Client) ListModels(ctx context.Context, purpose types.ModelPurpose) ([]types.ModelInfo, error) {
	if c.provider == string(catalog.Gemini) && purpose != types.PurposeAny {
		return c.listGeminiModelsByPurpose(ctx, purpose)
	}

	httpReq, err := c.newRequest(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling %s models endpoint: %w", c.provider, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeErrorBody(resp)
	}

	var out modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding %s models response: %w", c.provider, err)
	}

	models := make([]types.ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID == "" {
			continue
		}
		// Gemini's OpenAI-compatible endpoint namespaces IDs as "models/<id>";
		// strip it so the ID round-trips as a usable request model.
		id := strings.TrimPrefix(m.ID, "models/")
		models = append(models, types.ModelInfo{ID: id, Name: id})
	}

	return models, nil
}

// geminiNativeModelsURL derives Gemini's native models.list endpoint from the
// OpenAI-compatible base URL this client otherwise talks to
// (".../v1beta/openai" -> ".../v1beta/models"), rather than hardcoding a
// second host — only the native API reports each model's
// supportedGenerationMethods, which the OpenAI-compatible /models endpoint
// does not. Deriving it this way also means a workspace's custom baseURL
// override (or a test server) applies to both endpoints alike.
func (c *Client) geminiNativeModelsURL() string {
	return strings.TrimSuffix(c.baseURL, "/openai") + "/models"
}

type geminiModelsResponse struct {
	Models []struct {
		Name                       string   `json:"name"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	} `json:"models"`
}

// geminiGenerationMethod maps a ModelPurpose to the supportedGenerationMethods
// value Gemini's native API uses to report it.
func geminiGenerationMethod(purpose types.ModelPurpose) string {
	if purpose == types.PurposeEmbedding {
		return "embedContent"
	}
	return "generateContent"
}

// listGeminiModelsByPurpose lists Gemini's models via its native API and
// keeps only those whose supportedGenerationMethods cover purpose — excluding,
// for example, gemini-2.5-flash (chat-only) from an embedding picker, where it
// would otherwise appear selectable and then fail on the first embed call.
//
// pageSize is set to the API's max (1000) since Gemini's catalog is well
// under that, avoiding the need to page through pageToken. Auth is a "key"
// query parameter, not the Bearer header the OpenAI-compatible endpoint uses
// — the native API's own scheme.
func (c *Client) listGeminiModelsByPurpose(ctx context.Context, purpose types.ModelPurpose) ([]types.ModelInfo, error) {
	method := geminiGenerationMethod(purpose)

	reqURL := c.geminiNativeModelsURL() + "?pageSize=1000&key=" + url.QueryEscape(c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building gemini models request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling gemini models endpoint: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeErrorBody(resp)
	}

	var out geminiModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding gemini models response: %w", err)
	}

	models := make([]types.ModelInfo, 0, len(out.Models))
	for _, m := range out.Models {
		if m.Name == "" || !slices.Contains(m.SupportedGenerationMethods, method) {
			continue
		}
		id := strings.TrimPrefix(m.Name, "models/")
		models = append(models, types.ModelInfo{ID: id, Name: id})
	}

	return models, nil
}
