// Package modelconfig implements BYOK: it stores each workspace's provider API
// keys (encrypted) and its chosen completion and embedding models, and resolves
// them into ready-to-use clients at request time.
//
// It is the implementation behind llm.Resolver and embedding.Resolver. Callers
// in retrieval and sources depend on those interfaces, not on this package.
package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/catalog"
	"github.com/jpgomesr/NeuralVault/internal/config"
	"github.com/jpgomesr/NeuralVault/internal/crypto"
	"github.com/jpgomesr/NeuralVault/internal/embedding"
	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/storage"
	"github.com/jpgomesr/NeuralVault/internal/vectorstorage"
)

// ErrInvalidProvider is returned for a provider that does not exist, or that
// cannot do what it is being selected for (e.g. Anthropic as an embedder).
var ErrInvalidProvider = errors.New("invalid provider")

// ErrProviderUnavailable is returned when a provider's key is rejected or the
// provider cannot be reached. It is a user-facing configuration problem, not an
// internal error.
var ErrProviderUnavailable = errors.New("provider unavailable")

// ErrNoDefaultProvider is returned when a workspace has no provider configured
// for a role (completion or embedding) and the server has no default Ollama to
// fall back to (OLLAMA_URL unset — a fully BYOK deployment). The workspace must
// configure its own provider; there is nothing else to try.
var ErrNoDefaultProvider = errors.New("no default provider configured")

// Service is the model-configuration API of a workspace.
type Service interface {
	// Providers returns the provider catalog annotated with which ones this
	// workspace has a credential for. It never returns an API key.
	Providers(ctx context.Context, workspaceID uuid.UUID) ([]ProviderStatus, error)

	// SaveCredential validates apiKey against the provider and, only if it is
	// accepted, stores it encrypted.
	SaveCredential(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider, apiKey, baseURL string) error

	// DeleteCredential removes a workspace's key for a provider.
	DeleteCredential(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider) error

	// Models lists the models a workspace's credential can reach, live from the
	// provider.
	Models(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider) ([]llm.ModelInfo, error)

	// Settings returns a workspace's chosen models. Empty fields mean the
	// workspace is on the server default.
	Settings(ctx context.Context, workspaceID uuid.UUID) (model.WorkspaceModelSettings, error)

	// SetLLM sets the workspace's default completion provider and model.
	SetLLM(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider, llmModel string) error

	// SetEmbedding sets the workspace's embedding provider and model. It probes
	// the provider for the vector dimension, creates the matching Qdrant
	// collection, and reports whether existing sources must be re-indexed.
	SetEmbedding(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider, embeddingModel string) (EmbeddingChange, error)
}

// ProviderStatus is a catalog entry plus this workspace's credential state.
type ProviderStatus struct {
	catalog.Entry
	// Configured reports whether the workspace has a usable credential. Always
	// true for providers that need no key.
	Configured bool `json:"configured"`
	// APIKeyHint is the last few characters of the stored key, for display. The
	// key itself is never returned.
	APIKeyHint string `json:"api_key_hint,omitempty"`
	// BaseURL is the endpoint in effect: the workspace's override if it set one,
	// otherwise the catalog default.
	BaseURL string `json:"base_url,omitempty"`
}

// EmbeddingChange reports the consequences of switching embedding models.
type EmbeddingChange struct {
	// Collection is the Qdrant collection the workspace's vectors now live in.
	Collection string `json:"collection"`
	// Dimensions is the vector size discovered by probing the provider.
	Dimensions uint64 `json:"dimensions"`
	// ReindexRequired is true when the workspace has sources that were embedded
	// with a different model. Until they are re-indexed, the new collection is
	// empty and retrieval finds nothing.
	ReindexRequired bool `json:"reindex_required"`
	// StaleSources is how many sources need re-indexing.
	StaleSources int `json:"stale_sources"`
}

// ModelConfigService is the concrete Service. It also implements llm.Resolver
// and embedding.Resolver, by embedding the resolver.
type ModelConfigService struct {
	*resolver

	store       *store
	vectorStore vectorstorage.Client
	cfg         *config.Config
}

// NewModelConfigService constructs the service. cipher decrypts stored API keys;
// it comes from the SECRETS_ENCRYPTION_KEY master key.
func NewModelConfigService(pool storage.Pool, cipher *crypto.Cipher, vectorStore vectorstorage.Client, cfg *config.Config) *ModelConfigService {
	s := newStore(pool, cipher)
	return &ModelConfigService{
		resolver:    newResolver(s, cfg),
		store:       s,
		vectorStore: vectorStore,
		cfg:         cfg,
	}
}

// Providers returns the catalog annotated with this workspace's credentials.
func (s *ModelConfigService) Providers(ctx context.Context, workspaceID uuid.UUID) ([]ProviderStatus, error) {
	credentials, err := s.store.ListCredentials(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	saved := make(map[catalog.Provider]model.ProviderCredential, len(credentials))
	for _, c := range credentials {
		saved[c.Provider] = c
	}

	entries := catalog.All()
	out := make([]ProviderStatus, 0, len(entries))
	for _, entry := range entries {
		status := ProviderStatus{Entry: entry, BaseURL: entry.BaseURL}

		if !entry.RequiresAPIKey {
			// Ollama needs no key, but it still needs the server to have one
			// configured — an empty OLLAMA_URL means there is no default
			// provider, and the frontend must not offer it as selectable.
			status.Configured = s.cfg.Ollama.Enabled()
			status.BaseURL = s.cfg.Ollama.URL
			out = append(out, status)
			continue
		}

		if cred, ok := saved[entry.Provider]; ok {
			status.Configured = true
			status.APIKeyHint = cred.APIKeyHint
			if cred.BaseURL != "" {
				status.BaseURL = cred.BaseURL
			}
		}
		out = append(out, status)
	}

	return out, nil
}

// SaveCredential validates the key by asking the provider to list its models,
// then stores it encrypted.
//
// Validating first matters: a key saved without a probe would appear to work in
// the settings UI and only fail later, mid-answer, on a committed SSE stream
// where the error is much harder to surface.
func (s *ModelConfigService) SaveCredential(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider, apiKey, baseURL string) error {
	entry, ok := catalog.Lookup(provider)
	if !ok {
		return fmt.Errorf("%w: unknown provider %q", ErrInvalidProvider, provider)
	}

	if !entry.RequiresAPIKey {
		return fmt.Errorf("%w: provider %q takes no api key", ErrInvalidProvider, provider)
	}

	if apiKey == "" {
		return fmt.Errorf("%w: api key is empty", ErrInvalidProvider)
	}

	if baseURL == "" {
		baseURL = entry.BaseURL
	}

	if _, err := s.probeModels(ctx, entry, apiKey, baseURL); err != nil {
		return err
	}

	return s.store.SaveCredential(ctx, workspaceID, provider, apiKey, baseURL)
}

// probeModels lists a provider's models with a candidate credential, so a bad
// key is rejected before it is stored. It doubles as the model list itself.
func (s *ModelConfigService) probeModels(ctx context.Context, entry catalog.Entry, apiKey, baseURL string) ([]llm.ModelInfo, error) {
	provider, err := llm.New(ctx, llm.Credential{
		Provider: entry.Provider,
		APIKey:   apiKey,
		BaseURL:  baseURL,
	}, s.cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrProviderUnavailable, err)
	}

	lister, ok := provider.(llm.ModelLister)
	if !ok {
		// Every current provider lists models. A future one that cannot would
		// have to be validated some other way rather than silently accepted.
		return nil, fmt.Errorf("%w: provider %q cannot list models", ErrInvalidProvider, entry.Provider)
	}

	models, err := lister.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrProviderUnavailable, err)
	}

	return models, nil
}

// DeleteCredential removes a workspace's key for a provider.
func (s *ModelConfigService) DeleteCredential(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider) error {
	return s.store.DeleteCredential(ctx, workspaceID, provider)
}

// Models lists the models a workspace can reach on a provider, live.
func (s *ModelConfigService) Models(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider) ([]llm.ModelInfo, error) {
	entry, ok := catalog.Lookup(provider)
	if !ok {
		return nil, fmt.Errorf("%w: unknown provider %q", ErrInvalidProvider, provider)
	}

	apiKey, baseURL, err := s.credentialFor(ctx, workspaceID, entry)
	if err != nil {
		return nil, err
	}

	return s.probeModels(ctx, entry, apiKey, baseURL)
}

// Settings returns a workspace's chosen models.
func (s *ModelConfigService) Settings(ctx context.Context, workspaceID uuid.UUID) (model.WorkspaceModelSettings, error) {
	return s.store.GetSettings(ctx, workspaceID)
}

// SetLLM sets the workspace's default completion provider and model.
//
// Switching completion models has no effect on stored data — it only changes
// which model answers the next query — so unlike SetEmbedding there is nothing
// to migrate.
func (s *ModelConfigService) SetLLM(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider, llmModel string) error {
	entry, ok := catalog.Lookup(provider)
	if !ok {
		return fmt.Errorf("%w: unknown provider %q", ErrInvalidProvider, provider)
	}
	if !entry.SupportsCompletions {
		return fmt.Errorf("%w: provider %q does not serve completions", ErrInvalidProvider, provider)
	}
	if llmModel == "" {
		return fmt.Errorf("%w: model is empty", ErrInvalidProvider)
	}

	// Fails if the workspace has selected a provider it has no key for.
	if _, _, err := s.credentialFor(ctx, workspaceID, entry); err != nil {
		return err
	}

	return s.store.SaveLLMSettings(ctx, workspaceID, provider, llmModel)
}

// SetEmbedding sets the workspace's embedding provider and model.
//
// This is the expensive switch. A Qdrant collection has a fixed vector size, so
// a new embedding model needs a new collection, and every existing vector was
// produced by the old model and is meaningless in the new space. The flow is:
//
//  1. probe the provider for the vector dimension (this also validates the key),
//  2. create the collection named for that model and dimension,
//  3. persist the setting,
//  4. report how many sources are now stale.
//
// Re-indexing is deliberately NOT started here: it re-downloads and re-embeds
// every file, so it is the caller's (the user's) decision to trigger.
func (s *ModelConfigService) SetEmbedding(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider, embeddingModel string) (EmbeddingChange, error) {
	entry, ok := catalog.Lookup(provider)
	if !ok {
		return EmbeddingChange{}, fmt.Errorf("%w: unknown provider %q", ErrInvalidProvider, provider)
	}
	if !entry.SupportsEmbeddings {
		return EmbeddingChange{}, fmt.Errorf("%w: provider %q does not serve embeddings", ErrInvalidProvider, provider)
	}
	if embeddingModel == "" {
		return EmbeddingChange{}, fmt.Errorf("%w: model is empty", ErrInvalidProvider)
	}

	apiKey, baseURL, err := s.credentialFor(ctx, workspaceID, entry)
	if err != nil {
		return EmbeddingChange{}, err
	}

	dimensions, err := s.probeDimensions(ctx, entry, apiKey, baseURL, embeddingModel)
	if err != nil {
		return EmbeddingChange{}, err
	}

	collection := collectionName(provider, embeddingModel, dimensions)
	if err := vectorstorage.EnsureCollection(ctx, s.vectorStore, collection, dimensions); err != nil {
		return EmbeddingChange{}, fmt.Errorf("preparing collection for %s: %w", embeddingModel, err)
	}

	if err := s.store.SaveEmbeddingSettings(ctx, workspaceID, provider, embeddingModel, collection, dimensions); err != nil {
		return EmbeddingChange{}, err
	}

	stale, err := s.staleSourceCount(ctx, workspaceID, embeddingModel)
	if err != nil {
		return EmbeddingChange{}, err
	}

	return EmbeddingChange{
		Collection:      collection,
		Dimensions:      dimensions,
		ReindexRequired: stale > 0,
		StaleSources:    stale,
	}, nil
}

// probeText is embedded solely to learn a model's output dimension. Its content
// is irrelevant; only len(vector) is used.
const probeText = "dimension probe"

// probeDimensions asks the provider to embed one short string and measures the
// result.
//
// The dimension is discovered rather than looked up in a table because every
// provider and model has its own, they change, and getting it wrong is not a
// small error: the Qdrant collection would be created with the wrong vector size
// and reject every upsert at index time, long after the setting was saved.
func (s *ModelConfigService) probeDimensions(ctx context.Context, entry catalog.Entry, apiKey, baseURL, embeddingModel string) (uint64, error) {
	embedder, err := embedding.New(ctx, embedding.Credential{
		Provider: entry.Provider,
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Model:    embeddingModel,
	}, s.cfg)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrProviderUnavailable, err)
	}

	vector, err := embedder.Embed(ctx, probeText)
	if err != nil {
		return 0, fmt.Errorf("%w: probing %s dimensions: %s", ErrProviderUnavailable, embeddingModel, err)
	}
	if len(vector) == 0 {
		return 0, fmt.Errorf("%w: model %q returned an empty vector", ErrProviderUnavailable, embeddingModel)
	}

	return uint64(len(vector)), nil
}

// staleSourceCount counts a workspace's sources holding chunks embedded with a
// model other than the one now configured. Those sources' vectors are in the old
// collection (or the wrong space) and must be re-indexed to be searchable again.
func (s *ModelConfigService) staleSourceCount(ctx context.Context, workspaceID uuid.UUID, embeddingModel string) (int, error) {
	var count int
	err := s.store.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT source_id)
		FROM chunks
		WHERE workspace_id = $1 AND embedding_model <> $2`,
		workspaceID, embeddingModel,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting stale sources: %w", err)
	}
	return count, nil
}

// nonAlphanumeric matches every character that is not safe in a Qdrant
// collection name.
var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// collectionName derives a Qdrant collection name from the embedding model and
// its dimension, e.g. "nv_gemini_text_embedding_004_768".
//
// The dimension is part of the name on purpose: a collection's vector size is
// immutable, so encoding it means a name can never refer to a collection of the
// wrong size, and two models of different dimensions can never collide.
func collectionName(provider catalog.Provider, embeddingModel string, dimensions uint64) string {
	slug := nonAlphanumeric.ReplaceAllString(strings.ToLower(embeddingModel), "_")
	slug = strings.Trim(slug, "_")
	return fmt.Sprintf("nv_%s_%s_%d", provider, slug, dimensions)
}
