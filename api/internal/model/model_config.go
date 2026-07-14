package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/catalog"
)

// ProviderCredential is a workspace's API key for one provider (BYOK).
//
// APIKeyCiphertext holds the key sealed with AES-256-GCM (see internal/crypto);
// the plaintext key is never stored, logged, or returned over the API.
// APIKeyHint is the last few characters, kept so the UI can show which key is
// configured without exposing it.
type ProviderCredential struct {
	WorkspaceID      uuid.UUID        `db:"workspace_id"`
	Provider         catalog.Provider `db:"provider"`
	APIKeyCiphertext []byte           `db:"api_key_ciphertext"`
	APIKeyHint       string           `db:"api_key_hint"`
	// BaseURL overrides the catalog's default endpoint for self-hosted or
	// proxied deployments. Empty means use the catalog default.
	BaseURL   string    `db:"base_url"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// WorkspaceModelSettings is a workspace's chosen provider and model for
// completions and for embeddings.
//
// Every field is optional. A workspace with no settings — or with only one half
// set — falls back to the server's environment-configured default (Ollama) for
// whatever is missing.
//
// EmbeddingDimensions and EmbeddingCollection are stored rather than derived: a
// Qdrant collection is created with a fixed vector size, so each embedding model
// needs its own collection. The dimension is discovered by probing the provider
// when the setting is saved.
type WorkspaceModelSettings struct {
	WorkspaceID uuid.UUID `db:"workspace_id"`

	LLMProvider catalog.Provider `db:"llm_provider"`
	LLMModel    string           `db:"llm_model"`

	EmbeddingProvider   catalog.Provider `db:"embedding_provider"`
	EmbeddingModel      string           `db:"embedding_model"`
	EmbeddingDimensions uint64           `db:"embedding_dimensions"`
	EmbeddingCollection string           `db:"embedding_collection"`

	UpdatedAt time.Time `db:"updated_at"`
}

// HasLLM reports whether the workspace has chosen its own completion provider.
func (s WorkspaceModelSettings) HasLLM() bool {
	return s.LLMProvider != "" && s.LLMModel != ""
}

// HasEmbedding reports whether the workspace has chosen its own embedding
// provider. The collection and dimensions are set together with it, so a
// half-configured embedding setup is never considered valid.
func (s WorkspaceModelSettings) HasEmbedding() bool {
	return s.EmbeddingProvider != "" &&
		s.EmbeddingModel != "" &&
		s.EmbeddingCollection != "" &&
		s.EmbeddingDimensions > 0
}
