// Package reranking defines the reranker interface and a factory that returns
// the configured provider implementation.
// No business logic should depend on a concrete reranker — only this interface.
package reranking

import (
	"context"

	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/reranking/tei"
	"github.com/jpgomesr/neuralvault/api/internal/reranking/types"
)

// Candidate and Result are re-exported from the shared types package so
// callers only need to import this package.
type Candidate = types.Candidate
type Result = types.Result

// Reranker scores a set of candidate chunks against a query using a
// cross-encoder, which jointly attends to each (query, chunk) pair instead of
// comparing independently-computed embeddings — catching relevance that pure
// vector or lexical similarity misses.
//
// Implementations must be safe for concurrent use.
type Reranker interface {
	// Rerank scores each candidate's relevance to query. Results are not
	// guaranteed to preserve candidates' input order; correlate results back
	// to candidates via Result.CandidateID. If candidates is empty,
	// implementations should return an empty slice and no error.
	Rerank(ctx context.Context, query string, candidates []Candidate) ([]Result, error)

	// HealthCheck verifies the reranking backend is reachable. It reports
	// only reachability, not model identity, so it stays cheap enough for
	// /health.
	HealthCheck(ctx context.Context) error
}

// NewReranker creates and returns a Reranker backed by the configured provider.
func NewReranker(ctx context.Context, cfg *config.Config) (Reranker, error) {
	return tei.New(ctx, cfg)
}
