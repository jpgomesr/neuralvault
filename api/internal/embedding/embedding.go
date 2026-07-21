// Package embedding defines the embedder interface and the factories that
// return a concrete embedder implementation.
// No business logic should depend on a concrete embedder — only this interface.
package embedding

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/catalog"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/embedding/ollama"
	"github.com/jpgomesr/NeuralVault/internal/embedding/openaicompat"
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

// Credential is what it takes to talk to one embedding backend: which provider,
// with which key, at which endpoint.
type Credential struct {
	// Provider selects the backend. See internal/catalog.
	Provider catalog.Provider
	// APIKey authenticates the request. Empty for Ollama, which is unauthenticated.
	APIKey string
	// BaseURL overrides the catalog's default endpoint. Empty means use the default.
	BaseURL string
	// Model is the embedding model to use.
	Model string
}

// Target is everything the ingest and retrieval paths need to know about a
// workspace's embedding setup beyond the Embedder itself.
//
// Collection and Dimensions travel together with the Embedder because they are
// not independent: a Qdrant collection is created with a fixed vector size, so
// each embedding model requires its own collection. Mixing them would silently
// corrupt search — Qdrant would reject the upsert, or worse, compare vectors
// from different models.
type Target struct {
	// Model is the embedding model name, recorded on each chunk.
	Model string
	// Collection is the Qdrant collection holding this model's vectors.
	Collection string
	// Dimensions is the vector size the collection was created with.
	Dimensions uint64
}

// Resolver returns the Embedder a workspace should use, together with the
// collection its vectors live in.
//
// It is declared here, next to the interface it produces, so retrieval and
// sources depend only on this package rather than on the domain that stores
// credentials. Implemented by internal/modelconfig.
//
// Unlike llm.Resolver there is no per-request override: the embedder is bound
// to the workspace's collection, and swapping it for a single query would
// compare vectors against a collection built by a different model.
type Resolver interface {
	ResolveEmbedder(ctx context.Context, workspaceID uuid.UUID) (Embedder, Target, error)
}

// New builds an Embedder from a credential.
//
// Unlike NewEmbedder it performs no reachability check for remote providers: a
// workspace credential is validated once when the user saves it (see
// internal/modelconfig). The Ollama case delegates to the same constructor as
// the server default, which does check the model is pulled.
func New(ctx context.Context, cred Credential, cfg *config.Config) (Embedder, error) {
	entry, ok := catalog.Lookup(cred.Provider)
	if !ok {
		return nil, fmt.Errorf("unknown embedding provider %q", cred.Provider)
	}

	if !entry.SupportsEmbeddings {
		return nil, fmt.Errorf("provider %q does not serve embeddings", cred.Provider)
	}

	if entry.RequiresAPIKey && cred.APIKey == "" {
		return nil, fmt.Errorf("provider %q requires an api key", cred.Provider)
	}

	baseURL := cred.BaseURL
	if baseURL == "" {
		baseURL = entry.BaseURL
	}

	switch entry.Kind {
	case catalog.KindOllama:
		// Ollama is server-local: its URL comes from the server's own config,
		// not from a workspace credential.
		return ollama.NewWithModel(ctx, cfg.Ollama.URL, cred.Model)

	case catalog.KindOpenAICompat:
		return openaicompat.New(string(cred.Provider), baseURL, cred.APIKey, cred.Model), nil

	default:
		return nil, fmt.Errorf("unsupported embedding provider kind %q", entry.Kind)
	}
}

// NewEmbedder creates an Embedder backed by the server's environment-configured
// default (Ollama). It fails fast if the model is not pulled, so a
// misconfigured server dies at boot rather than on the first ingest.
func NewEmbedder(ctx context.Context, cfg *config.Config) (Embedder, error) {
	return ollama.New(ctx, cfg)
}
