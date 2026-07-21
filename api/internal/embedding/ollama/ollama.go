// Package ollama implements embedding.Embedder backed by an Ollama server.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/embedding/types"
)

// defaultHTTPTimeout bounds a single HTTP round-trip to Ollama independent of
// the caller's context, guarding against a stalled connection or a stuck model load.
const defaultHTTPTimeout = 60 * time.Second

// Client is an embedding.Embedder backed by an Ollama server's /api/embed endpoint.
type Client struct {
	httpClient *http.Client
	baseURL    string
	model      string
}

// New creates a Client and verifies the configured embedding model is
// available on the Ollama server before returning, so callers fail fast
// instead of discovering a missing model on the first real embed request.
func New(ctx context.Context, cfg *config.Config) (*Client, error) {
	return NewWithModel(ctx, cfg.Ollama.URL, cfg.Ollama.EmbeddingModel)
}

// NewWithModel is New for a model chosen at runtime rather than from the
// environment — the path a workspace takes when it selects an Ollama embedding
// model in the UI. The same fail-fast model check applies.
func NewWithModel(ctx context.Context, baseURL, model string) (*Client, error) {
	c := &Client{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		model:      model,
	}

	if err := c.ensureModelAvailable(ctx); err != nil {
		return nil, err
	}

	slog.Info("ollama embedder connected", "url", c.baseURL, "model", c.model)
	return c, nil
}

// HealthCheck verifies the Ollama server is reachable by requesting its tags
// endpoint. It deliberately checks only reachability (HTTP 200), not whether
// the configured model is present, so it stays cheap enough for /health.
func (c *Client) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("building ollama health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("reaching ollama: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health: unexpected status %d", resp.StatusCode)
	}
	return nil
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ensureModelAvailable confirms the configured embedding model has already
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
		if m.Name == c.model || strings.HasPrefix(m.Name, c.model+":") {
			return nil
		}
	}

	return fmt.Errorf("ollama model %q not found; pull it with `ollama pull %s`", c.model, c.model)
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

// nomic-embed-text was trained with task-instruction prefixes and expects
// them at inference time for good retrieval quality: queries and indexed
// documents occupy different regions of embedding space unless each is
// prefixed for its role. This is a quirk of this specific model, not a
// general Ollama behavior — see usesNomicPrefixes.
const (
	nomicEmbedTextModel = "nomic-embed-text"
	nomicQueryPrefix    = "search_query: "
	nomicDocumentPrefix = "search_document: "
)

// usesNomicPrefixes reports whether the configured embedding model is
// nomic-embed-text (bare name or any ":"-tagged variant, matching
// ensureModelAvailable's name comparison). Other Ollama-served embedding
// models (mxbai-embed-large, bge-*, all-minilm, ...) use different or no
// prefix conventions, so prefixing must not apply unconditionally.
func (c *Client) usesNomicPrefixes() bool {
	return c.model == nomicEmbedTextModel || strings.HasPrefix(c.model, nomicEmbedTextModel+":")
}

// Embed returns the vector for a single piece of text.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	if c.usesNomicPrefixes() {
		text = nomicQueryPrefix + text
	}
	vectors, err := c.embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

// EmbedBatch returns one Embedding for each Chunk, preserving input order.
func (c *Client) EmbedBatch(ctx context.Context, chunks []types.Chunk) ([]types.Embedding, error) {
	if len(chunks) == 0 {
		return []types.Embedding{}, nil
	}

	prefixDocs := c.usesNomicPrefixes()
	texts := make([]string, len(chunks))
	for i, ch := range chunks {
		if prefixDocs {
			texts[i] = nomicDocumentPrefix + ch.Text
		} else {
			texts[i] = ch.Text
		}
	}

	vectors, err := c.embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embedding batch of %d chunks: %w", len(chunks), err)
	}

	results := make([]types.Embedding, len(chunks))
	for i, ch := range chunks {
		results[i] = types.Embedding{ChunkID: ch.ID, Vector: vectors[i]}
	}
	return results, nil
}

// embed sends texts (in order) to Ollama's /api/embed and returns one vector
// per input, in the same order. It is the single code path shared by Embed
// and EmbedBatch.
func (c *Client) embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshaling ollama embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building ollama embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling ollama embed endpoint: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Error != "" {
			return nil, fmt.Errorf("ollama embed request failed (status %d): %s", resp.StatusCode, apiErr.Error)
		}
		return nil, fmt.Errorf("ollama embed request failed: unexpected status %d", resp.StatusCode)
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding ollama embed response: %w", err)
	}

	if err := validateEmbedResponse(out, len(texts)); err != nil {
		return nil, err
	}

	return out.Embeddings, nil
}

// validateEmbedResponse enforces the invariants embedding.Embedder callers
// depend on: one non-empty vector per input, in order.
func validateEmbedResponse(resp embedResponse, wantCount int) error {
	if len(resp.Embeddings) != wantCount {
		return fmt.Errorf(
			"ollama embed response: expected %d embeddings, got %d",
			wantCount, len(resp.Embeddings),
		)
	}
	for i, v := range resp.Embeddings {
		if len(v) == 0 {
			return fmt.Errorf("ollama embed response: embedding at index %d is empty", i)
		}
	}
	return nil
}
