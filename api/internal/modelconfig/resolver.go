package modelconfig

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/jpgomesr/neuralvault/api/internal/catalog"
	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/embedding"
	"github.com/jpgomesr/neuralvault/api/internal/llm"
	"github.com/jpgomesr/neuralvault/api/internal/model"
)

// clientKeyMACKey is a random key generated once per process, used only to
// derive clientKey's cache key from an API key via HMAC. It deliberately
// never leaves the process and is never persisted: a plain hash of the key
// alone would let anyone who ever observed a cache key (e.g. in a memory
// dump) brute-force it back to the underlying secret, since API keys don't
// carry the entropy a password-hashing KDF assumes. Keying the hash removes
// that risk without needing a slow KDF for what is otherwise just a
// process-local map key.
var clientKeyMACKey = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("modelconfig: generating client-key MAC key: " + err.Error())
	}
	return key
}()

// configStore is the persistence the resolver reads. It is an interface, not
// the concrete *store, so resolution can be tested without a database — the
// resolver's job is precedence and client construction, not SQL.
type configStore interface {
	GetSettings(ctx context.Context, workspaceID uuid.UUID) (model.WorkspaceModelSettings, error)
	GetCredential(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider) (apiKey, baseURL string, err error)
}

// resolver turns a workspace's stored settings into a ready-to-use provider.
//
// It implements both llm.Resolver and embedding.Resolver, so retrieval and
// sources depend on those small interfaces rather than on this domain.
type resolver struct {
	store configStore
	cfg   *config.Config

	// llmClients and embedderClients memoise built clients. Without this every
	// query would construct a fresh HTTP client, and for Ollama would re-run the
	// /api/tags model check — a network round-trip per request.
	//
	// The cache key includes a hash of the API key, so rotating a key naturally
	// misses the cache rather than serving a client built with the old one; no
	// explicit invalidation is needed on save. Stale entries are never reused
	// and the key space is bounded by the (provider, model, key) combinations a
	// deployment actually uses.
	llmClients      sync.Map // clientKey -> llm.Provider
	embedderClients sync.Map // clientKey -> embedding.Embedder
}

func newResolver(s configStore, cfg *config.Config) *resolver {
	return &resolver{store: s, cfg: cfg}
}

// clientKey identifies a built client by everything that affects its behaviour.
func clientKey(provider catalog.Provider, model, baseURL, apiKey string) string {
	mac := hmac.New(sha256.New, clientKeyMACKey)
	mac.Write([]byte(apiKey)) //nolint:errcheck // hash.Hash.Write never returns an error
	sum := mac.Sum(nil)
	return fmt.Sprintf("%s|%s|%s|%s", provider, model, baseURL, hex.EncodeToString(sum[:8]))
}

// credentialFor loads the workspace's key for a provider.
//
// Providers that need no key (Ollama) short-circuit — except Ollama itself when
// the server has none configured (OLLAMA_URL empty). That check belongs here
// rather than in each caller because every path that can end up choosing Ollama
// — an explicit override, saved workspace settings, or the implicit
// no-settings-configured default — funnels through this one function.
//
// A provider that needs a key but has none is a user-facing configuration
// error, not an internal failure: it means the workspace selected a model
// without saving a key for it.
func (r *resolver) credentialFor(ctx context.Context, workspaceID uuid.UUID, entry catalog.Entry) (apiKey, baseURL string, err error) {
	if !entry.RequiresAPIKey {
		if entry.Provider == catalog.Ollama && !r.cfg.Ollama.Enabled() {
			return "", "", fmt.Errorf("%w: this server has no default Ollama provider configured", ErrNoDefaultProvider)
		}
		return "", "", nil
	}

	apiKey, baseURL, err = r.store.GetCredential(ctx, workspaceID, entry.Provider)
	if errors.Is(err, ErrCredentialNotFound) {
		return "", "", fmt.Errorf("%w: no api key saved for provider %q", ErrCredentialNotFound, entry.Provider)
	}
	if err != nil {
		return "", "", err
	}

	if baseURL == "" {
		baseURL = entry.BaseURL
	}
	return apiKey, baseURL, nil
}

// ResolveLLM returns the completion provider and model for a workspace.
//
// Precedence: a non-nil override (the model picker in the chat composer) beats
// the workspace's persisted default, which beats the server's environment
// default. An override still has to name a provider the workspace holds a
// credential for — it selects among configured providers, it does not bypass
// configuration.
func (r *resolver) ResolveLLM(ctx context.Context, workspaceID uuid.UUID, override *llm.Selection) (llm.Provider, string, error) {
	provider, model, err := r.llmSelection(ctx, workspaceID, override)
	if err != nil {
		return nil, "", err
	}

	entry, ok := catalog.Lookup(provider)
	if !ok {
		return nil, "", fmt.Errorf("unknown llm provider %q", provider)
	}

	apiKey, baseURL, err := r.credentialFor(ctx, workspaceID, entry)
	if err != nil {
		return nil, "", err
	}

	key := clientKey(provider, model, baseURL, apiKey)
	if cached, ok := r.llmClients.Load(key); ok {
		return cached.(llm.Provider), model, nil
	}

	client, err := llm.New(ctx, llm.Credential{
		Provider: provider,
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Model:    model,
	}, r.cfg)
	if err != nil {
		return nil, "", fmt.Errorf("building %s provider: %w", provider, err)
	}

	// LoadOrStore, not Store: two concurrent queries for the same workspace can
	// race here, and both must end up using the same client.
	actual, _ := r.llmClients.LoadOrStore(key, client)
	return actual.(llm.Provider), model, nil
}

// llmSelection applies the override → workspace default → server default
// precedence.
func (r *resolver) llmSelection(ctx context.Context, workspaceID uuid.UUID, override *llm.Selection) (catalog.Provider, string, error) {
	if override != nil && override.Provider != "" && override.Model != "" {
		return override.Provider, override.Model, nil
	}

	settings, err := r.store.GetSettings(ctx, workspaceID)
	if err != nil {
		return "", "", err
	}

	if settings.HasLLM() {
		return settings.LLMProvider, settings.LLMModel, nil
	}

	return catalog.Ollama, r.cfg.Ollama.CompletionModel, nil
}

// ResolveEmbedder returns the embedder for a workspace, together with the
// Qdrant collection its vectors live in.
//
// There is no override parameter, unlike ResolveLLM: the embedder is bound to
// the collection, and embedding a query with a different model than the one that
// built the collection compares vectors from two incompatible spaces, silently
// returning nonsense. Changing it is a settings operation that forces a
// re-index, never a per-request choice.
func (r *resolver) ResolveEmbedder(ctx context.Context, workspaceID uuid.UUID) (embedding.Embedder, embedding.Target, error) {
	settings, err := r.store.GetSettings(ctx, workspaceID)
	if err != nil {
		return nil, embedding.Target{}, err
	}

	// No embedding settings: the workspace is on the server default, which uses
	// the single collection created at boot.
	if !settings.HasEmbedding() {
		target := embedding.Target{
			Model:      r.cfg.Ollama.EmbeddingModel,
			Collection: r.cfg.Qdrant.CollectionName,
			Dimensions: r.cfg.Qdrant.VectorSize,
		}
		embedder, err := r.embedderFor(ctx, workspaceID, catalog.Ollama, target.Model)
		if err != nil {
			return nil, embedding.Target{}, err
		}
		return embedder, target, nil
	}

	target := embedding.Target{
		Model:      settings.EmbeddingModel,
		Collection: settings.EmbeddingCollection,
		Dimensions: settings.EmbeddingDimensions,
	}

	embedder, err := r.embedderFor(ctx, workspaceID, settings.EmbeddingProvider, settings.EmbeddingModel)
	if err != nil {
		return nil, embedding.Target{}, err
	}
	return embedder, target, nil
}

// embedderFor builds (or reuses) the embedding client for a provider/model.
func (r *resolver) embedderFor(ctx context.Context, workspaceID uuid.UUID, provider catalog.Provider, model string) (embedding.Embedder, error) {
	entry, ok := catalog.Lookup(provider)
	if !ok {
		return nil, fmt.Errorf("unknown embedding provider %q", provider)
	}

	apiKey, baseURL, err := r.credentialFor(ctx, workspaceID, entry)
	if err != nil {
		return nil, err
	}

	key := clientKey(provider, model, baseURL, apiKey)
	if cached, ok := r.embedderClients.Load(key); ok {
		return cached.(embedding.Embedder), nil
	}

	client, err := embedding.New(ctx, embedding.Credential{
		Provider: provider,
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Model:    model,
	}, r.cfg)
	if err != nil {
		return nil, fmt.Errorf("building %s embedder: %w", provider, err)
	}

	actual, _ := r.embedderClients.LoadOrStore(key, client)
	return actual.(embedding.Embedder), nil
}
