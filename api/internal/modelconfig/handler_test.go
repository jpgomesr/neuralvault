package modelconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jpgomesr/neuralvault/api/internal/catalog"
	"github.com/jpgomesr/neuralvault/api/internal/llm"
	"github.com/jpgomesr/neuralvault/api/internal/model"
)

type errTest string

func (e errTest) Error() string { return string(e) }

// fakeService is a Service test double whose method results are set per test.
type fakeService struct {
	providers    []ProviderStatus
	providersErr error

	saveCredentialErr error
	gotProvider       catalog.Provider
	gotAPIKey         string
	gotBaseURL        string

	deleteCredentialErr error

	models    []llm.ModelInfo
	modelsErr error

	settings    model.WorkspaceModelSettings
	settingsErr error

	setLLMErr  error
	gotLLMModel string

	embeddingChange EmbeddingChange
	setEmbeddingErr error
}

func (f *fakeService) Providers(context.Context, uuid.UUID) ([]ProviderStatus, error) {
	return f.providers, f.providersErr
}

func (f *fakeService) SaveCredential(_ context.Context, _ uuid.UUID, provider catalog.Provider, apiKey, baseURL string) error {
	f.gotProvider, f.gotAPIKey, f.gotBaseURL = provider, apiKey, baseURL
	return f.saveCredentialErr
}

func (f *fakeService) DeleteCredential(context.Context, uuid.UUID, catalog.Provider) error {
	return f.deleteCredentialErr
}

func (f *fakeService) Models(context.Context, uuid.UUID, catalog.Provider) ([]llm.ModelInfo, error) {
	return f.models, f.modelsErr
}

func (f *fakeService) Settings(context.Context, uuid.UUID) (model.WorkspaceModelSettings, error) {
	return f.settings, f.settingsErr
}

func (f *fakeService) SetLLM(_ context.Context, _ uuid.UUID, provider catalog.Provider, llmModel string) error {
	f.gotProvider, f.gotLLMModel = provider, llmModel
	return f.setLLMErr
}

func (f *fakeService) SetEmbedding(context.Context, uuid.UUID, catalog.Provider, string) (EmbeddingChange, error) {
	return f.embeddingChange, f.setEmbeddingErr
}

// fakeReindexer is a Reindexer test double.
type fakeReindexer struct {
	queued int
	err    error
}

func (f *fakeReindexer) ReindexWorkspace(context.Context, uuid.UUID) (int, error) {
	return f.queued, f.err
}

// fakeMembers is a workspaces.Service test double controlling whether the
// caller is treated as a member of the queried workspace.
type fakeMembers struct {
	member bool
	err    error
}

func (f fakeMembers) Create(context.Context, uuid.UUID, string) (*model.Workspace, error) {
	return nil, nil
}
func (f fakeMembers) List(context.Context, uuid.UUID) ([]model.Workspace, error) { return nil, nil }
func (f fakeMembers) IsMember(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.member, f.err
}

func allowMembers() fakeMembers { return fakeMembers{member: true} }

// newRequest builds a request carrying chi URL params the way the real router
// would after matching /workspaces/{workspace_id}/providers/{provider}/...;
// handler.go reads both exclusively via chi.URLParam.
func newRequest(method, body string, params map[string]string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, "/", bytes.NewBufferString(body))
	} else {
		r = httptest.NewRequest(method, "/", nil)
	}
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func wsParams(workspaceID uuid.UUID) map[string]string {
	return map[string]string{"workspace_id": workspaceID.String()}
}

func wsProviderParams(workspaceID uuid.UUID, provider catalog.Provider) map[string]string {
	return map[string]string{"workspace_id": workspaceID.String(), "provider": string(provider)}
}

// --- workspace_id / membership guard, exercised once via ListProviders and
// once via a write endpoint (SaveCredential) since every handler shares the
// same h.workspaceID helper. ---

func TestListProviders_InvalidWorkspaceID(t *testing.T) {
	h := NewHandler(&fakeService{}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.ListProviders(w, newRequest(http.MethodGet, "", map[string]string{"workspace_id": "not-a-uuid"}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListProviders_Forbidden(t *testing.T) {
	h := NewHandler(&fakeService{}, fakeMembers{member: false}, &fakeReindexer{})

	w := httptest.NewRecorder()
	h.ListProviders(w, newRequest(http.MethodGet, "", wsParams(uuid.New())))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestListProviders_Success(t *testing.T) {
	fake := &fakeService{providers: []ProviderStatus{
		{Entry: catalog.Entry{Provider: catalog.Anthropic}, Configured: true, APIKeyHint: "abcd"},
	}}
	h := NewHandler(fake, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.ListProviders(w, newRequest(http.MethodGet, "", wsParams(uuid.New())))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got []ProviderStatus
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Provider != catalog.Anthropic || !got[0].Configured || got[0].APIKeyHint != "abcd" {
		t.Fatalf("unexpected body: %+v", got)
	}
}

func TestListProviders_ServiceError(t *testing.T) {
	h := NewHandler(&fakeService{providersErr: errTest("connection refused")}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.ListProviders(w, newRequest(http.MethodGet, "", wsParams(uuid.New())))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "connection refused") {
		t.Fatalf("body leaked internal detail: %q", w.Body.String())
	}
}

// --- SaveCredential ---

func TestSaveCredential_InvalidWorkspaceID(t *testing.T) {
	h := NewHandler(&fakeService{}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	body := `{"api_key":"sk-test"}`
	h.SaveCredential(w, newRequest(http.MethodPut, body, map[string]string{"workspace_id": "not-a-uuid", "provider": "anthropic"}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSaveCredential_Forbidden(t *testing.T) {
	h := NewHandler(&fakeService{}, fakeMembers{member: false}, &fakeReindexer{})

	w := httptest.NewRecorder()
	body := `{"api_key":"sk-test"}`
	h.SaveCredential(w, newRequest(http.MethodPut, body, wsProviderParams(uuid.New(), catalog.Anthropic)))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestSaveCredential_Success(t *testing.T) {
	fake := &fakeService{}
	h := NewHandler(fake, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	body := `{"api_key":"sk-test","base_url":"https://example.com"}`
	h.SaveCredential(w, newRequest(http.MethodPut, body, wsProviderParams(uuid.New(), catalog.Anthropic)))

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if fake.gotProvider != catalog.Anthropic || fake.gotAPIKey != "sk-test" || fake.gotBaseURL != "https://example.com" {
		t.Fatalf("service did not receive the expected arguments: provider=%q apiKey=%q baseURL=%q", fake.gotProvider, fake.gotAPIKey, fake.gotBaseURL)
	}
}

func TestSaveCredential_InvalidBody(t *testing.T) {
	h := NewHandler(&fakeService{}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.SaveCredential(w, newRequest(http.MethodPut, "not json", wsProviderParams(uuid.New(), catalog.Anthropic)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSaveCredential_EmptyAPIKey(t *testing.T) {
	h := NewHandler(&fakeService{}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.SaveCredential(w, newRequest(http.MethodPut, `{"api_key":""}`, wsProviderParams(uuid.New(), catalog.Anthropic)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestSaveCredential_ErrorMapping verifies writeServiceError's classification:
// user-configuration errors (ErrInvalidProvider) are a safe 400, an upstream
// rejection (ErrProviderUnavailable) is a safe 502, and anything else is a
// generic, non-leaking 500 — this is the same taxonomy relied on by the
// retrieval handler's credential-error fix.
func TestSaveCredential_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantLeaked bool
	}{
		{name: "invalid provider", err: ErrInvalidProvider, wantStatus: http.StatusBadRequest, wantLeaked: true},
		{name: "provider unavailable", err: ErrProviderUnavailable, wantStatus: http.StatusBadGateway, wantLeaked: true},
		{name: "no default provider", err: ErrNoDefaultProvider, wantStatus: http.StatusBadRequest, wantLeaked: true},
		{name: "generic internal error", err: errTest("pgx: connection refused"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&fakeService{saveCredentialErr: tt.err}, allowMembers(), &fakeReindexer{})

			w := httptest.NewRecorder()
			h.SaveCredential(w, newRequest(http.MethodPut, `{"api_key":"sk-test"}`, wsProviderParams(uuid.New(), catalog.Anthropic)))

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
			}
			leaked := strings.Contains(w.Body.String(), tt.err.Error())
			if leaked != tt.wantLeaked {
				t.Fatalf("body = %q, leaked = %v, want %v", w.Body.String(), leaked, tt.wantLeaked)
			}
		})
	}
}

// --- DeleteCredential ---

func TestDeleteCredential_Success(t *testing.T) {
	h := NewHandler(&fakeService{}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.DeleteCredential(w, newRequest(http.MethodDelete, "", wsProviderParams(uuid.New(), catalog.Anthropic)))

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteCredential_ServiceError(t *testing.T) {
	h := NewHandler(&fakeService{deleteCredentialErr: errTest("connection refused")}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.DeleteCredential(w, newRequest(http.MethodDelete, "", wsProviderParams(uuid.New(), catalog.Anthropic)))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// --- ListModels ---

func TestListModels_Success(t *testing.T) {
	fake := &fakeService{models: []llm.ModelInfo{{ID: "claude-sonnet-5", Name: "Claude Sonnet 5"}}}
	h := NewHandler(fake, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.ListModels(w, newRequest(http.MethodGet, "", wsProviderParams(uuid.New(), catalog.Anthropic)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got []llm.ModelInfo
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ID != "claude-sonnet-5" {
		t.Fatalf("unexpected body: %+v", got)
	}
}

// TestListModels_CredentialConfigError verifies the same safe-400 mapping
// SaveCredential gets also applies to the read path a chat session actually
// hits when listing models for the picker.
func TestListModels_CredentialConfigError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "credential not found", err: ErrCredentialNotFound},
		{name: "no default provider", err: ErrNoDefaultProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&fakeService{modelsErr: tt.err}, allowMembers(), &fakeReindexer{})

			w := httptest.NewRecorder()
			h.ListModels(w, newRequest(http.MethodGet, "", wsProviderParams(uuid.New(), catalog.Anthropic)))

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// --- GetSettings ---

func TestGetSettings_Success(t *testing.T) {
	fake := &fakeService{settings: model.WorkspaceModelSettings{
		LLMProvider: catalog.Anthropic, LLMModel: "claude-sonnet-5",
	}}
	h := NewHandler(fake, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.GetSettings(w, newRequest(http.MethodGet, "", wsParams(uuid.New())))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got settingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.LLMProvider != catalog.Anthropic || got.LLMModel != "claude-sonnet-5" {
		t.Fatalf("unexpected body: %+v", got)
	}
	// Unset embedding fields must be omitted (omitempty), the client's signal
	// that the workspace is on the server default.
	if strings.Contains(w.Body.String(), "embedding_provider") {
		t.Fatalf("expected empty embedding fields to be omitted, got %q", w.Body.String())
	}
}

func TestGetSettings_ServiceError(t *testing.T) {
	h := NewHandler(&fakeService{settingsErr: errTest("connection refused")}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.GetSettings(w, newRequest(http.MethodGet, "", wsParams(uuid.New())))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// --- SetLLM ---

func TestSetLLM_Success(t *testing.T) {
	fake := &fakeService{}
	h := NewHandler(fake, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	body := `{"provider":"anthropic","model":"claude-sonnet-5"}`
	h.SetLLM(w, newRequest(http.MethodPut, body, wsParams(uuid.New())))

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if fake.gotProvider != catalog.Anthropic || fake.gotLLMModel != "claude-sonnet-5" {
		t.Fatalf("service did not receive the expected arguments: provider=%q model=%q", fake.gotProvider, fake.gotLLMModel)
	}
}

func TestSetLLM_InvalidBody(t *testing.T) {
	h := NewHandler(&fakeService{}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	h.SetLLM(w, newRequest(http.MethodPut, "not json", wsParams(uuid.New())))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSetLLM_InvalidProvider(t *testing.T) {
	h := NewHandler(&fakeService{setLLMErr: ErrInvalidProvider}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	body := `{"provider":"not-a-provider","model":"x"}`
	h.SetLLM(w, newRequest(http.MethodPut, body, wsParams(uuid.New())))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// --- SetEmbedding ---

func TestSetEmbedding_Success(t *testing.T) {
	fake := &fakeService{embeddingChange: EmbeddingChange{
		Collection: "nv_openai_text_embedding_3_small_1536", Dimensions: 1536, ReindexRequired: true, StaleSources: 2,
	}}
	h := NewHandler(fake, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	body := `{"provider":"openai","model":"text-embedding-3-small"}`
	h.SetEmbedding(w, newRequest(http.MethodPut, body, wsParams(uuid.New())))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got EmbeddingChange
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != fake.embeddingChange {
		t.Fatalf("body = %+v, want %+v", got, fake.embeddingChange)
	}
}

func TestSetEmbedding_ProviderUnavailable(t *testing.T) {
	h := NewHandler(&fakeService{setEmbeddingErr: ErrProviderUnavailable}, allowMembers(), &fakeReindexer{})

	w := httptest.NewRecorder()
	body := `{"provider":"openai","model":"text-embedding-3-small"}`
	h.SetEmbedding(w, newRequest(http.MethodPut, body, wsParams(uuid.New())))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Reindex ---

func TestReindex_Success(t *testing.T) {
	h := NewHandler(&fakeService{}, allowMembers(), &fakeReindexer{queued: 3})

	w := httptest.NewRecorder()
	h.Reindex(w, newRequest(http.MethodPost, "", wsParams(uuid.New())))

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var got reindexResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Queued != 3 {
		t.Fatalf("queued = %d, want 3", got.Queued)
	}
}

func TestReindex_ServiceError(t *testing.T) {
	h := NewHandler(&fakeService{}, allowMembers(), &fakeReindexer{err: errTest("connection refused")})

	w := httptest.NewRecorder()
	h.Reindex(w, newRequest(http.MethodPost, "", wsParams(uuid.New())))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}
