// Package tei implements reranking.Reranker backed by a Hugging Face Text
// Embeddings Inference (TEI) server's /rerank endpoint.
package tei

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/reranking/types"
)

// defaultHTTPTimeout bounds a single HTTP round-trip to the reranker
// independent of the caller's context, guarding against a stalled connection.
const defaultHTTPTimeout = 30 * time.Second

// Client is a reranking.Reranker backed by a TEI server's /rerank endpoint.
type Client struct {
	httpClient *http.Client
	baseURL    string
	model      string
}

// New creates a Client and verifies the TEI server is serving the configured
// model before returning, so callers fail fast instead of discovering a
// misconfigured reranker on the first real request.
func New(ctx context.Context, cfg *config.Config) (*Client, error) {
	c := &Client{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:    cfg.Reranker.URL,
		model:      cfg.Reranker.Model,
	}

	if err := c.ensureModelAvailable(ctx); err != nil {
		return nil, err
	}

	slog.Info("reranker connected", "url", c.baseURL, "model", c.model)
	return c, nil
}

// HealthCheck verifies the TEI server is reachable via its /health endpoint.
// It deliberately checks only reachability, not model identity, so it stays
// cheap enough for /health.
func (c *Client) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("building reranker health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("reaching reranker: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reranker health: unexpected status %d", resp.StatusCode)
	}
	return nil
}

type infoResponse struct {
	ModelID string `json:"model_id"`
}

// ensureModelAvailable confirms the TEI server is serving the model this
// Client was configured for. Unlike Ollama (one server, many pullable
// models), a TEI instance only ever serves the single model passed via
// --model-id at container startup — so "is my model available" here means
// "is the endpoint up and serving what I expect," not "has it been pulled."
func (c *Client) ensureModelAvailable(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/info", nil)
	if err != nil {
		return fmt.Errorf("building reranker info request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("checking reranker availability: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reranker info check: unexpected status %d", resp.StatusCode)
	}

	var info infoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return fmt.Errorf("decoding reranker info response: %w", err)
	}

	if info.ModelID != c.model {
		return fmt.Errorf(
			"reranker at %s is serving model %q, expected %q (set RERANKER_MODEL to match, or restart the reranker container with --model-id %s)",
			c.baseURL, info.ModelID, c.model, c.model,
		)
	}

	return nil
}

type rerankRequest struct {
	Query     string   `json:"query"`
	Texts     []string `json:"texts"`
	RawScores bool     `json:"raw_scores"`
}

type rerankResult struct {
	Index int     `json:"index"`
	Score float32 `json:"score"`
}

// Rerank implements reranking.Reranker.
func (c *Client) Rerank(ctx context.Context, query string, candidates []types.Candidate) ([]types.Result, error) {
	if len(candidates) == 0 {
		return []types.Result{}, nil
	}

	texts := make([]string, len(candidates))
	for i, cand := range candidates {
		texts[i] = cand.Text
	}

	body, err := json.Marshal(rerankRequest{Query: query, Texts: texts, RawScores: false})
	if err != nil {
		return nil, fmt.Errorf("marshaling rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling reranker: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Error != "" {
			return nil, fmt.Errorf("rerank request failed (status %d): %s", resp.StatusCode, apiErr.Error)
		}
		return nil, fmt.Errorf("rerank request failed: unexpected status %d", resp.StatusCode)
	}

	var results []rerankResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decoding rerank response: %w", err)
	}

	out := make([]types.Result, 0, len(results))
	for _, r := range results {
		if r.Index < 0 || r.Index >= len(candidates) {
			return nil, fmt.Errorf("rerank response: index %d out of range for %d candidates", r.Index, len(candidates))
		}
		out = append(out, types.Result{CandidateID: candidates[r.Index].ID, Score: r.Score})
	}
	return out, nil
}
