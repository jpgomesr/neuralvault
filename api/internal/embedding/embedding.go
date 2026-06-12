// Package embedding defines the embedder interface and domain types for
// generating semantic vector representations of text.
// No business logic should depend on a concrete embedder — only this interface.
package embedding

import "context"

// Chunk is the atomic unit of text that gets embedded.
// It carries the original text together with provenance metadata
// so the retrieval layer can reconstruct the source after a vector search.
type Chunk struct {
	ID       string         // stable identifier used to correlate chunks with their vectors
	Text     string         // raw text to be embedded
	Source   string         // origin of the text (file path, URL, git ref, …)
	Metadata map[string]any // arbitrary indexer-specific fields (page number, heading, …)
}

// Embedding is the vector representation produced for a Chunk.
type Embedding struct {
	ChunkID string    // references Chunk.ID
	Vector  []float32 // dense vector; length is model-specific (e.g. 768 for nomic-embed-text)
}

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
}
