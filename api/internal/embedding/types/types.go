// Package types defines the shared domain types for embedding input and output.
// It has no internal imports so both the embedding interface package and concrete
// provider packages can import it without creating an import cycle.
package types

// Chunk is the atomic unit of text that gets embedded.
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
