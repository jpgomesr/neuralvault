package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

// fakeService is a minimal test double for Service.
type fakeService struct {
	source *model.Source
	chunks []model.Chunk
	err    error
}

func (f *fakeService) Create(_ context.Context, _ CreateRequest, _ []FileUpload) (*model.Source, error) {
	return f.source, f.err
}

func (f *fakeService) Ingest(_ context.Context, _ uuid.UUID) error {
	return f.err
}

func (f *fakeService) List(_ context.Context, _ uuid.UUID) ([]model.Source, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.source != nil {
		return []model.Source{*f.source}, nil
	}
	return []model.Source{}, nil
}

func (f *fakeService) ListChunks(_ context.Context, _ uuid.UUID) ([]model.Chunk, error) {
	return f.chunks, f.err
}

func (f *fakeService) GetByID(_ context.Context, _ uuid.UUID) (*model.Source, error) {
	return f.source, f.err
}

// routedRequest attaches Chi URL params to an httptest request.
func routedRequest(method, target string, params map[string]string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// multipartUpload builds a multipart/form-data request with form fields and one file.
func multipartUpload(fields map[string]string, fileName, fileContent string) *http.Request {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if fileName != "" {
		fw, _ := mw.CreateFormFile("files", fileName)
		_, _ = io.WriteString(fw, fileContent)
	}
	mw.Close()

	r := httptest.NewRequest(http.MethodPost, "/sources", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func sourceWithStatus(status model.SourceStatus) *model.Source {
	return &model.Source{
		ID:          uuid.New(),
		WorkspaceID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Name:        "test",
		Type:        model.SourceTypeFile,
		Status:      status,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// ── CreateSource ─────────────────────────────────────────────────────────────

func TestCreateSource_Success(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexing)
	h := NewHandler(&fakeService{source: src}, NewProgressBus())

	r := multipartUpload(map[string]string{
		"workspace_id": "11111111-1111-1111-1111-111111111111",
		"name":         "My Vault",
	}, "notes.md", "# Hello")
	w := httptest.NewRecorder()
	h.CreateSource(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["source"]; !ok {
		t.Error("response missing 'source'")
	}
	if _, ok := resp["status_url"]; !ok {
		t.Error("response missing 'status_url'")
	}
}

func TestCreateSource_InvalidWorkspaceID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus())

	r := multipartUpload(map[string]string{
		"workspace_id": "not-a-uuid",
		"name":         "vault",
	}, "f.md", "data")
	w := httptest.NewRecorder()
	h.CreateSource(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateSource_MissingName(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus())

	r := multipartUpload(map[string]string{
		"workspace_id": "11111111-1111-1111-1111-111111111111",
	}, "f.md", "data")
	w := httptest.NewRecorder()
	h.CreateSource(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateSource_NoFiles(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus())

	r := multipartUpload(map[string]string{
		"workspace_id": "11111111-1111-1111-1111-111111111111",
		"name":         "vault",
	}, "", "")
	w := httptest.NewRecorder()
	h.CreateSource(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateSource_ServiceError(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("create failed")}, NewProgressBus())

	r := multipartUpload(map[string]string{
		"workspace_id": "11111111-1111-1111-1111-111111111111",
		"name":         "vault",
	}, "f.md", "data")
	w := httptest.NewRecorder()
	h.CreateSource(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── ListSources ───────────────────────────────────────────────────────────────

func TestListSources_Success(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus())

	r := httptest.NewRequest(http.MethodGet, "/sources?workspace_id=11111111-1111-1111-1111-111111111111", nil)
	w := httptest.NewRecorder()
	h.ListSources(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var sources []model.Source
	if err := json.NewDecoder(w.Body).Decode(&sources); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
}

func TestListSources_InvalidWorkspaceID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus())

	r := httptest.NewRequest(http.MethodGet, "/sources?workspace_id=bad", nil)
	w := httptest.NewRecorder()
	h.ListSources(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListSources_ServiceError(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("db error")}, NewProgressBus())

	r := httptest.NewRequest(http.MethodGet, "/sources?workspace_id=11111111-1111-1111-1111-111111111111", nil)
	w := httptest.NewRecorder()
	h.ListSources(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── IngestSource ─────────────────────────────────────────────────────────────

func TestIngestSource_Success(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus())
	id := uuid.New()

	r := routedRequest(http.MethodPost, "/sources/"+id.String()+"/ingest",
		map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.IngestSource(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIngestSource_InvalidID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus())

	r := routedRequest(http.MethodPost, "/sources/bad/ingest", map[string]string{"id": "bad"}, nil)
	w := httptest.NewRecorder()
	h.IngestSource(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestIngestSource_ServiceError(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("ingest error")}, NewProgressBus())
	id := uuid.New()

	r := routedRequest(http.MethodPost, "/sources/"+id.String()+"/ingest",
		map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.IngestSource(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── ListChunks ────────────────────────────────────────────────────────────────

func TestListChunks_Success(t *testing.T) {
	chunks := []model.Chunk{{ID: uuid.New(), Content: "hello"}}
	h := NewHandler(&fakeService{chunks: chunks}, NewProgressBus())
	id := uuid.New()

	r := routedRequest(http.MethodGet, "/sources/"+id.String()+"/chunks",
		map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.ListChunks(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got []model.Chunk
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(got))
	}
}

func TestListChunks_InvalidID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus())

	r := routedRequest(http.MethodGet, "/sources/bad/chunks", map[string]string{"id": "bad"}, nil)
	w := httptest.NewRecorder()
	h.ListChunks(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListChunks_ServiceError(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("db error")}, NewProgressBus())
	id := uuid.New()

	r := routedRequest(http.MethodGet, "/sources/"+id.String()+"/chunks",
		map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.ListChunks(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── StreamStatus ─────────────────────────────────────────────────────────────

func TestStreamStatus_InvalidID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus())

	r := routedRequest(http.MethodGet, "/sources/bad/status", map[string]string{"id": "bad"}, nil)
	w := httptest.NewRecorder()
	h.StreamStatus(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStreamStatus_SourceNotFound(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("not found")}, NewProgressBus())
	id := uuid.New()

	r := routedRequest(http.MethodGet, "/sources/"+id.String()+"/status",
		map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.StreamStatus(w, r)

	if !strings.Contains(w.Body.String(), `"type":"error"`) {
		t.Errorf("expected error event in body, got: %s", w.Body.String())
	}
}

func TestStreamStatus_AlreadyIndexed(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus())

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/status",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.StreamStatus(w, r)

	body := w.Body.String()
	if !strings.Contains(body, `"type":"done"`) {
		t.Errorf("expected done event, got: %s", body)
	}
}

func TestStreamStatus_AlreadyErrored(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusError)
	h := NewHandler(&fakeService{source: src}, NewProgressBus())

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/status",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.StreamStatus(w, r)

	body := w.Body.String()
	if !strings.Contains(body, `"type":"error"`) {
		t.Errorf("expected error event, got: %s", body)
	}
}

// TestStreamStatus_LiveEvents verifies that the SSE handler delivers live events
// published to the bus while the source is in indexing state.
func TestStreamStatus_LiveEvents(t *testing.T) {
	bus := NewProgressBus()
	src := sourceWithStatus(model.SourceStatusIndexing)
	h := NewHandler(&fakeService{source: src}, bus)

	router := chi.NewRouter()
	router.Get("/{id}/status", h.StreamStatus)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	// publish indexing + done events after a short delay
	go func() {
		time.Sleep(30 * time.Millisecond)
		bus.publish(src.ID, ProgressEvent{Type: EventIndexing, File: "notes.md", Chunks: 2})
		bus.publish(src.ID, ProgressEvent{Type: EventDone, Total: 2})
	}()

	resp, err := http.Get(srv.URL + "/" + src.ID.String() + "/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !strings.Contains(string(body), `"type":"indexing"`) {
		t.Errorf("expected indexing event in SSE stream, got: %s", body)
	}
	if !strings.Contains(string(body), `"type":"done"`) {
		t.Errorf("expected done event in SSE stream, got: %s", body)
	}
}

// errTest is a simple error type for test service errors.
type errTest string

func (e errTest) Error() string { return string(e) }
