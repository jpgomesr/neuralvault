// Package llm defines the provider interface and the factories that return a
// concrete provider implementation.
// No business logic should depend on a concrete provider — only this interface.
package llm

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jpgomesr/neuralvault/api/internal/catalog"
	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/llm/anthropic"
	"github.com/jpgomesr/neuralvault/api/internal/llm/ollama"
	"github.com/jpgomesr/neuralvault/api/internal/llm/openaicompat"
	"github.com/jpgomesr/neuralvault/api/internal/llm/types"
)

// Role, Message, CompletionRequest, CompletionResponse, Usage, StreamChunk and
// ModelInfo are re-exported from the shared types package so callers only need
// to import this package.
type (
	Role               = types.Role
	Message            = types.Message
	CompletionRequest  = types.CompletionRequest
	CompletionResponse = types.CompletionResponse
	Usage              = types.Usage
	StreamChunk        = types.StreamChunk
	ModelInfo          = types.ModelInfo
	ModelPurpose       = types.ModelPurpose
)

const (
	PurposeAny        = types.PurposeAny        // no filtering — used only to probe a credential
	PurposeCompletion = types.PurposeCompletion // models usable for chat completions
	PurposeEmbedding  = types.PurposeEmbedding  // models usable for embeddings
)

const (
	RoleSystem    = types.RoleSystem    // instructions that frame the model's behaviour
	RoleUser      = types.RoleUser      // input from the end user
	RoleAssistant = types.RoleAssistant // previous model turn, used for multi-turn context
)

// Provider is the single abstraction over any LLM backend
// (OpenAI, Claude, Gemini, Ollama, …).
//
// Implementations must be safe for concurrent use.
type Provider interface {
	// Complete sends a blocking request and returns the full response.
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)

	// Stream returns a channel that emits incremental chunks as the model generates them.
	// The channel is closed after the chunk with Done == true is sent.
	// Cancelling ctx stops generation and closes the channel early.
	Stream(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error)
}

// ModelLister is an optional capability: a Provider that can enumerate the
// models its credential can reach, optionally filtered to those usable for
// purpose. A provider that cannot filter (most of them — see ModelPurpose)
// ignores purpose and returns its full list.
//
// It is kept separate from Provider so the interface every caller depends on
// stays limited to what retrieval actually needs. Consumers type-assert for it.
// All three current implementations satisfy it.
type ModelLister interface {
	ListModels(ctx context.Context, purpose ModelPurpose) ([]ModelInfo, error)
}

// Credential is what it takes to talk to one provider: which backend, with
// which key, at which endpoint. It is the input to New.
type Credential struct {
	// Provider selects the backend. See internal/catalog.
	Provider catalog.Provider
	// APIKey authenticates the request. Empty for Ollama, which is unauthenticated.
	APIKey string
	// BaseURL overrides the catalog's default endpoint. Empty means use the default.
	BaseURL string
	// Model is the default model for requests that do not set one themselves.
	Model string
}

// Selection is a caller's choice of provider and model, used to override a
// workspace's persisted default for a single request.
type Selection struct {
	Provider catalog.Provider
	Model    string
}

// Resolver returns the Provider a workspace should use.
//
// It is declared here, next to the interface it produces, so retrieval and
// sources depend only on this package rather than on the domain that stores
// credentials. Implemented by internal/modelconfig.
type Resolver interface {
	// ResolveLLM returns the provider and the model name to use for a
	// workspace. A non-nil override takes precedence over the workspace's
	// persisted default; it must name a provider the workspace has a
	// credential for.
	ResolveLLM(ctx context.Context, workspaceID uuid.UUID, override *Selection) (Provider, string, error)
}

// New builds a Provider from a credential.
//
// Unlike NewProvider it performs no reachability check: a workspace credential
// is validated once when the user saves it (see internal/modelconfig), and
// re-probing on every construction would add a round-trip to each request.
// The Ollama case is the exception — it delegates to the same constructor as
// the server default, which does check the model is pulled.
func New(ctx context.Context, cred Credential, cfg *config.Config) (Provider, error) {
	entry, ok := catalog.Lookup(cred.Provider)
	if !ok {
		return nil, fmt.Errorf("unknown llm provider %q", cred.Provider)
	}

	if !entry.SupportsCompletions {
		return nil, fmt.Errorf("provider %q does not serve completions", cred.Provider)
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

	case catalog.KindAnthropic:
		return anthropic.New(baseURL, cred.APIKey, cred.Model), nil

	case catalog.KindOpenAICompat:
		return openaicompat.New(string(cred.Provider), baseURL, cred.APIKey, cred.Model), nil

	default:
		return nil, fmt.Errorf("unsupported llm provider kind %q", entry.Kind)
	}
}

// NewProvider creates a Provider backed by the server's environment-configured
// default (Ollama). It fails fast if the model is not pulled, so a
// misconfigured server dies at boot rather than on the first query.
//
// This fail-fast contract applies only to the server default. A workspace's own
// BYOK credential cannot fail startup — it does not exist yet at boot — so New
// deliberately does not check reachability.
func NewProvider(ctx context.Context, cfg *config.Config) (Provider, error) {
	return ollama.New(ctx, cfg)
}
