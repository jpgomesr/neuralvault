package modelconfig

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/jpgomesr/neuralvault/api/internal/catalog"
	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/llm"
	"github.com/jpgomesr/neuralvault/api/internal/model"
)

// fakeStore stands in for Postgres, so these tests cover precedence and client
// construction rather than SQL.
type fakeStore struct {
	settings    model.WorkspaceModelSettings
	credentials map[catalog.Provider]string
	baseURLs    map[catalog.Provider]string
}

func (f fakeStore) GetSettings(context.Context, uuid.UUID) (model.WorkspaceModelSettings, error) {
	return f.settings, nil
}

func (f fakeStore) GetCredential(_ context.Context, _ uuid.UUID, provider catalog.Provider) (string, string, error) {
	key, ok := f.credentials[provider]
	if !ok {
		return "", "", ErrCredentialNotFound
	}
	return key, f.baseURLs[provider], nil
}

// ollamaServer serves the /api/tags response the Ollama client checks on
// construction, so the server-default path can be exercised without Ollama.
func ollamaServer(t *testing.T) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3:latest"},{"name":"nomic-embed-text:latest"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()

	return &config.Config{
		Ollama: config.Ollama{
			URL:             ollamaServer(t),
			CompletionModel: "llama3",
			EmbeddingModel:  "nomic-embed-text",
		},
		Qdrant: config.Qdrant{
			CollectionName: "neuralvault",
			VectorSize:     768,
		},
	}
}

// A workspace with no settings must fall back to the server's environment
// default. This is what keeps every existing workspace working after the BYOK
// migration, which leaves their settings rows absent.
func TestResolveLLM_FallsBackToServerDefault(t *testing.T) {
	r := newResolver(fakeStore{}, testConfig(t))

	_, gotModel, err := r.ResolveLLM(context.Background(), uuid.New(), nil)
	if err != nil {
		t.Fatalf("ResolveLLM: %v", err)
	}
	if gotModel != "llama3" {
		t.Errorf("model = %q, want the server default %q", gotModel, "llama3")
	}
}

// A fully BYOK deployment (OLLAMA_URL unset) has no fallback: a workspace with
// no settings and no override must get a clear, actionable error instead of an
// attempt to reach an empty URL.
func TestResolveLLM_NoDefaultProviderWhenOllamaDisabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.Ollama = config.Ollama{} // OLLAMA_URL unset: disabled.
	r := newResolver(fakeStore{}, cfg)

	_, _, err := r.ResolveLLM(context.Background(), uuid.New(), nil)
	if !errors.Is(err, ErrNoDefaultProvider) {
		t.Fatalf("error = %v, want ErrNoDefaultProvider", err)
	}
}

// Explicitly picking "ollama" — via an override or saved settings — must be
// rejected the same way when the server has none configured, not just the
// implicit fallback.
func TestResolveLLM_ExplicitOllamaRejectedWhenDisabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.Ollama = config.Ollama{}
	r := newResolver(fakeStore{}, cfg)

	override := &llm.Selection{Provider: catalog.Ollama, Model: "llama3"}
	_, _, err := r.ResolveLLM(context.Background(), uuid.New(), override)
	if !errors.Is(err, ErrNoDefaultProvider) {
		t.Fatalf("error = %v, want ErrNoDefaultProvider", err)
	}
}

func TestResolveEmbedder_NoDefaultProviderWhenOllamaDisabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.Ollama = config.Ollama{}
	r := newResolver(fakeStore{}, cfg)

	_, _, err := r.ResolveEmbedder(context.Background(), uuid.New())
	if !errors.Is(err, ErrNoDefaultProvider) {
		t.Fatalf("error = %v, want ErrNoDefaultProvider", err)
	}
}

func TestResolveLLM_UsesWorkspaceDefault(t *testing.T) {
	store := fakeStore{
		settings: model.WorkspaceModelSettings{
			LLMProvider: catalog.Groq,
			LLMModel:    "llama-3.3-70b",
		},
		credentials: map[catalog.Provider]string{catalog.Groq: "gsk-test"},
	}
	r := newResolver(store, testConfig(t))

	_, gotModel, err := r.ResolveLLM(context.Background(), uuid.New(), nil)
	if err != nil {
		t.Fatalf("ResolveLLM: %v", err)
	}
	if gotModel != "llama-3.3-70b" {
		t.Errorf("model = %q, want the workspace default", gotModel)
	}
}

// The chat composer's model picker sends an override, which must win over the
// saved default without changing it.
func TestResolveLLM_OverrideWinsOverWorkspaceDefault(t *testing.T) {
	store := fakeStore{
		settings: model.WorkspaceModelSettings{
			LLMProvider: catalog.Groq,
			LLMModel:    "llama-3.3-70b",
		},
		credentials: map[catalog.Provider]string{
			catalog.Groq:      "gsk-test",
			catalog.Anthropic: "sk-ant-test",
		},
	}
	r := newResolver(store, testConfig(t))

	override := &llm.Selection{Provider: catalog.Anthropic, Model: "claude-sonnet-5"}
	_, gotModel, err := r.ResolveLLM(context.Background(), uuid.New(), override)
	if err != nil {
		t.Fatalf("ResolveLLM: %v", err)
	}
	if gotModel != "claude-sonnet-5" {
		t.Errorf("model = %q, want the override", gotModel)
	}
}

// An override selects among the providers a workspace has configured — it must
// not be a way to use a provider with no key.
func TestResolveLLM_OverrideWithoutCredentialIsRejected(t *testing.T) {
	r := newResolver(fakeStore{}, testConfig(t))

	override := &llm.Selection{Provider: catalog.Anthropic, Model: "claude-sonnet-5"}
	_, _, err := r.ResolveLLM(context.Background(), uuid.New(), override)
	if !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("error = %v, want ErrCredentialNotFound", err)
	}
}

// A half-specified override (provider without model, or vice versa) is ignored
// rather than half-applied, which would pair a provider with another provider's
// model name.
func TestResolveLLM_PartialOverrideIsIgnored(t *testing.T) {
	r := newResolver(fakeStore{}, testConfig(t))

	override := &llm.Selection{Provider: catalog.Anthropic}
	_, gotModel, err := r.ResolveLLM(context.Background(), uuid.New(), override)
	if err != nil {
		t.Fatalf("ResolveLLM: %v", err)
	}
	if gotModel != "llama3" {
		t.Errorf("model = %q, want the server default (partial override ignored)", gotModel)
	}
}

// Building a client is not free — for Ollama it costs an /api/tags round-trip —
// so repeated resolution of the same workspace must reuse one client.
func TestResolveLLM_CachesClients(t *testing.T) {
	r := newResolver(fakeStore{}, testConfig(t))
	ctx := context.Background()
	id := uuid.New()

	first, _, err := r.ResolveLLM(ctx, id, nil)
	if err != nil {
		t.Fatalf("ResolveLLM: %v", err)
	}
	second, _, err := r.ResolveLLM(ctx, id, nil)
	if err != nil {
		t.Fatalf("ResolveLLM: %v", err)
	}

	if first != second {
		t.Error("resolving twice built two clients, want the cached one")
	}
}

func TestResolveEmbedder_FallsBackToServerCollection(t *testing.T) {
	cfg := testConfig(t)
	r := newResolver(fakeStore{}, cfg)

	_, target, err := r.ResolveEmbedder(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ResolveEmbedder: %v", err)
	}

	if target.Collection != cfg.Qdrant.CollectionName {
		t.Errorf("collection = %q, want the server default %q", target.Collection, cfg.Qdrant.CollectionName)
	}
	if target.Dimensions != cfg.Qdrant.VectorSize {
		t.Errorf("dimensions = %d, want %d", target.Dimensions, cfg.Qdrant.VectorSize)
	}
	if target.Model != cfg.Ollama.EmbeddingModel {
		t.Errorf("model = %q, want %q", target.Model, cfg.Ollama.EmbeddingModel)
	}
}

// The embedder and its collection must always travel together: a workspace on
// its own embedding model must never be handed the server's default collection,
// whose vectors came from a different model.
func TestResolveEmbedder_UsesWorkspaceCollection(t *testing.T) {
	store := fakeStore{
		settings: model.WorkspaceModelSettings{
			EmbeddingProvider:   catalog.Gemini,
			EmbeddingModel:      "text-embedding-004",
			EmbeddingCollection: "nv_gemini_text_embedding_004_768",
			EmbeddingDimensions: 768,
		},
		credentials: map[catalog.Provider]string{catalog.Gemini: "aiza-test"},
	}
	r := newResolver(store, testConfig(t))

	_, target, err := r.ResolveEmbedder(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ResolveEmbedder: %v", err)
	}

	if target.Collection != "nv_gemini_text_embedding_004_768" {
		t.Errorf("collection = %q, want the workspace's own collection", target.Collection)
	}
	if target.Model != "text-embedding-004" {
		t.Errorf("model = %q, want the workspace's embedding model", target.Model)
	}
}

// Half-written embedding settings must not be trusted: without a collection or
// a dimension there is nowhere valid to search, so the server default stands.
func TestResolveEmbedder_IncompleteSettingsFallBack(t *testing.T) {
	cfg := testConfig(t)
	store := fakeStore{
		settings: model.WorkspaceModelSettings{
			EmbeddingProvider: catalog.Gemini,
			EmbeddingModel:    "text-embedding-004",
			// No collection, no dimensions.
		},
		credentials: map[catalog.Provider]string{catalog.Gemini: "aiza-test"},
	}
	r := newResolver(store, cfg)

	_, target, err := r.ResolveEmbedder(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ResolveEmbedder: %v", err)
	}
	if target.Collection != cfg.Qdrant.CollectionName {
		t.Errorf("collection = %q, want the server default", target.Collection)
	}
}

func TestCollectionName(t *testing.T) {
	tests := map[string]struct {
		provider   catalog.Provider
		model      string
		dimensions uint64
		want       string
	}{
		"simple": {catalog.Gemini, "text-embedding-004", 768, "nv_gemini_text_embedding_004_768"},
		"slashes and case": {
			catalog.OpenAI, "Text-Embedding-3/Large", 3072, "nv_openai_text_embedding_3_large_3072",
		},
		"ollama tag": {catalog.Ollama, "nomic-embed-text:latest", 768, "nv_ollama_nomic_embed_text_latest_768"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := collectionName(tt.provider, tt.model, tt.dimensions); got != tt.want {
				t.Errorf("collectionName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The dimension is in the name precisely so two models can never share a
// collection sized for only one of them.
func TestCollectionName_DimensionSeparatesModels(t *testing.T) {
	small := collectionName(catalog.OpenAI, "text-embedding-3-large", 768)
	large := collectionName(catalog.OpenAI, "text-embedding-3-large", 3072)

	if small == large {
		t.Fatal("the same model at two dimensions produced one collection name")
	}
}
