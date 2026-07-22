package modelconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	qdrantpb "github.com/qdrant/go-client/qdrant"

	"github.com/jpgomesr/neuralvault/api/internal/catalog"
	"github.com/jpgomesr/neuralvault/api/internal/config"
	"github.com/jpgomesr/neuralvault/api/internal/crypto"
	"github.com/jpgomesr/neuralvault/api/internal/llm"
	"github.com/jpgomesr/neuralvault/api/internal/vectorstorage"
)

// fakeVectorStore is a vectorstorage.Client test double. SetEmbedding's call
// to vectorstorage.EnsureCollection only ever reaches CollectionExists and
// CreateCollection; the rest are stubbed solely to satisfy the interface.
type fakeVectorStore struct {
	exists       bool
	createCalled bool
	createErr    error
	gotCreate    *qdrantpb.CreateCollection
}

func (f *fakeVectorStore) HealthCheck(context.Context) (*qdrantpb.HealthCheckReply, error) {
	return nil, nil
}
func (f *fakeVectorStore) CollectionExists(context.Context, string) (bool, error) {
	return f.exists, nil
}
func (f *fakeVectorStore) CreateCollection(_ context.Context, req *qdrantpb.CreateCollection) error {
	f.createCalled = true
	f.gotCreate = req
	return f.createErr
}
func (f *fakeVectorStore) DeleteCollection(context.Context, string) error { return nil }
func (f *fakeVectorStore) Upsert(context.Context, *qdrantpb.UpsertPoints) (*qdrantpb.UpdateResult, error) {
	return nil, nil
}
func (f *fakeVectorStore) Query(context.Context, *qdrantpb.QueryPoints) ([]*qdrantpb.ScoredPoint, error) {
	return nil, nil
}
func (f *fakeVectorStore) Delete(context.Context, *qdrantpb.DeletePoints) (*qdrantpb.UpdateResult, error) {
	return nil, nil
}
func (f *fakeVectorStore) Count(context.Context, *qdrantpb.CountPoints) (uint64, error) {
	return 0, nil
}
func (f *fakeVectorStore) Close() error { return nil }

var _ vectorstorage.Client = (*fakeVectorStore)(nil)

// fakeProviderServer stands in for an OpenAI-compatible provider so
// SaveCredential/Models/SetEmbedding can be driven end-to-end — probing,
// persisting, and reading back — without any real network call. A request
// without the expected Authorization header gets a 401, exercising the
// invalid-key path the same way a real provider would.
func fakeProviderServer(t *testing.T, wantAPIKey string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+wantAPIKey {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "invalid api key"}})
			return
		}
		switch r.URL.Path {
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "test-model-1"}, {"id": "test-model-2"}},
			})
		case "/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"index": 0, "embedding": []float32{0.1, 0.2, 0.3, 0.4}}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newIntegrationService(t *testing.T, vs vectorstorage.Client) *ModelConfigService {
	t.Helper()
	cipher, err := crypto.New(testEncryptionKey)
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	return NewModelConfigService(sharedPool, cipher, vs, &config.Config{})
}

func TestSaveCredential_ProbesAndPersists(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderServer(t, "sk-good-key")
	s := newIntegrationService(t, &fakeVectorStore{})
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-good-key", srv.URL); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}

	apiKey, baseURL, err := s.store.GetCredential(ctx, wsID, catalog.OpenAI)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if apiKey != "sk-good-key" || baseURL != srv.URL {
		t.Errorf("got apiKey=%q baseURL=%q", apiKey, baseURL)
	}
}

// TestSaveCredential_RejectsInvalidKey verifies the doc comment on
// SaveCredential: a key that fails the probe must never reach storage.
func TestSaveCredential_RejectsInvalidKey(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderServer(t, "sk-good-key")
	s := newIntegrationService(t, &fakeVectorStore{})
	wsID := insertWorkspace(ctx, t)

	err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-bad-key", srv.URL)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}

	if _, _, err := s.store.GetCredential(ctx, wsID, catalog.OpenAI); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("credential was persisted despite failing the probe: err = %v", err)
	}
}

func TestModels_ListsLiveFromProvider(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderServer(t, "sk-good-key")
	s := newIntegrationService(t, &fakeVectorStore{})
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-good-key", srv.URL); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}

	models, err := s.Models(ctx, wsID, catalog.OpenAI, llm.PurposeAny)
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 || models[0].ID != "test-model-1" {
		t.Errorf("unexpected models: %+v", models)
	}
}

func TestProviders_AnnotatesConfiguredState(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderServer(t, "sk-good-key")
	s := newIntegrationService(t, &fakeVectorStore{})
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-good-key", srv.URL); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}

	statuses, err := s.Providers(ctx, wsID)
	if err != nil {
		t.Fatalf("Providers: %v", err)
	}

	var openai *ProviderStatus
	for i := range statuses {
		if statuses[i].Provider == catalog.OpenAI {
			openai = &statuses[i]
		}
	}
	if openai == nil {
		t.Fatal("openai not found in provider list")
	}
	if !openai.Configured || openai.APIKeyHint != "-key" {
		t.Errorf("unexpected status: %+v", openai)
	}
}

func TestDeleteCredential_RemovesStoredKey(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderServer(t, "sk-good-key")
	s := newIntegrationService(t, &fakeVectorStore{})
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-good-key", srv.URL); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	if err := s.DeleteCredential(ctx, wsID, catalog.OpenAI); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if _, _, err := s.store.GetCredential(ctx, wsID, catalog.OpenAI); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("err = %v, want ErrCredentialNotFound after delete", err)
	}
}

func TestSetLLM_PersistsAfterCredentialCheck(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderServer(t, "sk-good-key")
	s := newIntegrationService(t, &fakeVectorStore{})
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-good-key", srv.URL); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	if err := s.SetLLM(ctx, wsID, catalog.OpenAI, "gpt-test"); err != nil {
		t.Fatalf("SetLLM: %v", err)
	}

	settings, err := s.Settings(ctx, wsID)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if settings.LLMProvider != catalog.OpenAI || settings.LLMModel != "gpt-test" {
		t.Errorf("unexpected settings: %+v", settings)
	}
}

// TestSetLLM_RejectsProviderWithNoCredential guards the doc comment on SetLLM:
// choosing a provider the workspace has no key for must fail, not silently
// select it.
func TestSetLLM_RejectsProviderWithNoCredential(t *testing.T) {
	ctx := context.Background()
	s := newIntegrationService(t, &fakeVectorStore{})
	wsID := insertWorkspace(ctx, t)

	err := s.SetLLM(ctx, wsID, catalog.OpenAI, "gpt-test")
	if !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("err = %v, want ErrCredentialNotFound", err)
	}
}

func TestSetEmbedding_ProbesCreatesCollectionAndPersists(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderServer(t, "sk-good-key")
	vs := &fakeVectorStore{exists: false}
	s := newIntegrationService(t, vs)
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-good-key", srv.URL); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}

	change, err := s.SetEmbedding(ctx, wsID, catalog.OpenAI, "text-embedding-test")
	if err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}
	if change.Dimensions != 4 {
		t.Errorf("Dimensions = %d, want 4 (the probe vector's length)", change.Dimensions)
	}
	if !vs.createCalled {
		t.Error("expected CreateCollection to be called for a new collection")
	}
	if change.ReindexRequired || change.StaleSources != 0 {
		t.Errorf("expected no stale sources for a workspace with no chunks, got %+v", change)
	}

	settings, err := s.Settings(ctx, wsID)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if settings.EmbeddingProvider != catalog.OpenAI || settings.EmbeddingModel != "text-embedding-test" || settings.EmbeddingDimensions != 4 {
		t.Errorf("unexpected settings: %+v", settings)
	}
}

// TestSetEmbedding_SkipsCreateWhenCollectionExists guards
// vectorstorage.EnsureCollection's short-circuit: SetEmbedding must not try
// to recreate a collection a previous call (or another workspace on the same
// model+dimension) already created.
func TestSetEmbedding_SkipsCreateWhenCollectionExists(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderServer(t, "sk-good-key")
	vs := &fakeVectorStore{exists: true}
	s := newIntegrationService(t, vs)
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-good-key", srv.URL); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	if _, err := s.SetEmbedding(ctx, wsID, catalog.OpenAI, "text-embedding-test"); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}
	if vs.createCalled {
		t.Error("CreateCollection should not be called when the collection already exists")
	}
}

// TestResolveLLM_BuildsRealClientAndCaches exercises the resolver's actual
// client-construction and caching path (unlike resolver_test.go, which uses a
// fake store to isolate precedence logic from persistence and network).
func TestResolveLLM_BuildsRealClientAndCaches(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderServer(t, "sk-good-key")
	s := newIntegrationService(t, &fakeVectorStore{})
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-good-key", srv.URL); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	if err := s.SetLLM(ctx, wsID, catalog.OpenAI, "gpt-test"); err != nil {
		t.Fatalf("SetLLM: %v", err)
	}

	provider, model, err := s.ResolveLLM(ctx, wsID, nil)
	if err != nil {
		t.Fatalf("ResolveLLM: %v", err)
	}
	if model != "gpt-test" {
		t.Errorf("model = %q, want %q", model, "gpt-test")
	}
	if provider == nil {
		t.Fatal("provider is nil")
	}

	provider2, _, err := s.ResolveLLM(ctx, wsID, nil)
	if err != nil {
		t.Fatalf("ResolveLLM (2nd call): %v", err)
	}
	if provider != provider2 {
		t.Error("expected the cached client to be reused across calls")
	}
}

func TestResolveEmbedder_BuildsRealClient(t *testing.T) {
	ctx := context.Background()
	srv := fakeProviderServer(t, "sk-good-key")
	s := newIntegrationService(t, &fakeVectorStore{})
	wsID := insertWorkspace(ctx, t)

	if err := s.SaveCredential(ctx, wsID, catalog.OpenAI, "sk-good-key", srv.URL); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	if _, err := s.SetEmbedding(ctx, wsID, catalog.OpenAI, "text-embedding-test"); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}

	embedder, target, err := s.ResolveEmbedder(ctx, wsID)
	if err != nil {
		t.Fatalf("ResolveEmbedder: %v", err)
	}
	if target.Model != "text-embedding-test" || target.Dimensions != 4 {
		t.Errorf("unexpected target: %+v", target)
	}

	vector, err := embedder.Embed(ctx, "probe")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vector) != 4 {
		t.Errorf("len(vector) = %d, want 4", len(vector))
	}
}
