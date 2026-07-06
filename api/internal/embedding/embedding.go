// Package embedding defines the embedder interface and a factory that returns
// the configured provider implementation.
// No business logic should depend on a concrete embedder — only this interface.
package embedding

import (
	"context"

	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/embedding/ollama"
	"github.com/jpgomesr/NeuralVault/internal/embedding/types"
)

// Chunk and Embedding are re-exported from the shared types package so callers
// only need to import this package.
type Chunk = types.Chunk
type Embedding = types.Embedding

// Embedder is the single abstraction over any embedding backend
// (Ollama, OpenAI, Gemini, …).
//
// Implementations must be safe for concurrent use.
type Embedder interface {
	// Embed returns the vector for a single piece of text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch returns one Embedding for each Chunk.
	// Returned embeddings must preserve the input order and reference the
	// originating Chunk through Embedding.ChunkID.
	// Prefer this over repeated Embed calls to minimise round-trips to the provider.
	// If chunks is empty, implementations should return an empty slice and no error.
	EmbedBatch(ctx context.Context, chunks []Chunk) ([]Embedding, error)

	// HealthCheck verifies the embedding backend is reachable. It reports only
	// reachability, not model availability, so it stays cheap enough for /health.
	HealthCheck(ctx context.Context) error
}

// NewEmbedder creates and returns an Embedder backed by the configured provider.
func NewEmbedder(ctx context.Context, cfg *config.Config) (Embedder, error) {
	return ollama.New(ctx, cfg)
}
