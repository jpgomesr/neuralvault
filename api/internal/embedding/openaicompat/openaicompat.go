// Package openaicompat implements embedding.Embedder against any
// OpenAI-compatible /embeddings endpoint.
//
// This covers Google AI Studio (Gemini) and OpenAI. It deliberately does not
// cover Groq, OpenRouter or GitHub Models: those serve chat models only, which
// is why internal/catalog marks them SupportsEmbeddings: false.
package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jpgomesr/NeuralVault/internal/embedding/types"
)

// defaultHTTPTimeout bounds a single round-trip independent of the caller's
// context. Unlike a completion, an embedding request has a bounded expected
// duration, so a fixed timeout is safe here — the same reasoning as the Ollama
// embedding client.
const defaultHTTPTimeout = 60 * time.Second

// Client is an embedding.Embedder backed by an OpenAI-compatible embeddings API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
	// provider names the concrete backend (gemini, openai) so errors identify
	// who actually failed.
	provider string
}

// New creates a Client. It performs no startup check: a BYOK credential is
// validated when the user saves it (see internal/modelconfig), and a bad key
// surfaces as an error from the first embed call.
func New(provider, baseURL, apiKey, model string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
		provider:   provider,
	}
}

// HealthCheck verifies the provider is reachable and the key is accepted, by
// listing models. It checks only reachability, not that c.model exists, so it
// stays cheap — matching the Ollama embedder's contract.
func (c *Client) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("building %s health request: %w", c.provider, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("reaching %s: %w", c.provider, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s health: unexpected status %d", c.provider, resp.StatusCode)
	}
	return nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedResponse mirrors the OpenAI embeddings schema. Index matters: the spec
// permits the provider to return objects out of order, so the vectors are
// re-sorted by it rather than trusted positionally.
type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed returns the vector for a single piece of text.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
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

	texts := make([]string, len(chunks))
	for i, ch := range chunks {
		texts[i] = ch.Text
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

// embed sends texts to /embeddings and returns one vector per input, in the
// same order. It is the single code path shared by Embed and EmbedBatch.
func (c *Client) embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshaling %s embed request: %w", c.provider, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building %s embed request: %w", c.provider, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s embed endpoint: %w", c.provider, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Error.Message != "" {
			return nil, fmt.Errorf("%s embed request failed (status %d): %s", c.provider, resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("%s embed request failed: unexpected status %d", c.provider, resp.StatusCode)
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding %s embed response: %w", c.provider, err)
	}

	return c.toVectors(out, len(texts))
}

// toVectors enforces the invariants embedding.Embedder callers depend on — one
// non-empty vector per input, in input order — re-ordering by the response's
// own index field first.
func (c *Client) toVectors(resp embedResponse, wantCount int) ([][]float32, error) {
	if len(resp.Data) != wantCount {
		return nil, fmt.Errorf(
			"%s embed response: expected %d embeddings, got %d",
			c.provider, wantCount, len(resp.Data),
		)
	}

	sort.Slice(resp.Data, func(i, j int) bool { return resp.Data[i].Index < resp.Data[j].Index })

	vectors := make([][]float32, wantCount)
	for i, d := range resp.Data {
		if len(d.Embedding) == 0 {
			return nil, fmt.Errorf("%s embed response: embedding at index %d is empty", c.provider, i)
		}
		vectors[i] = d.Embedding
	}

	return vectors, nil
}
