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

// testMaxUpload is a generous upload cap used by handler tests that aren't
// exercising the MaxBytesReader limit.
const testMaxUpload int64 = 32 << 20

// fakeService is a minimal test double for Service.
type fakeService struct {
	source      *model.Source
	chunks      []model.Chunk
	files       []model.SourceFile
	fileContent string
	fileType    string
	err         error
	ingestErr   error
	filesErr    error
	openErr     error
	deleteErr   error
}

func (f *fakeService) Create(_ context.Context, _ CreateRequest, _ []FileUpload) (*model.Source, error) {
	return f.source, f.err
}

func (f *fakeService) Ingest(_ context.Context, _ uuid.UUID) error {
	return f.ingestErr
}

func (f *fakeService) Delete(_ context.Context, _ uuid.UUID) error {
	return f.deleteErr
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

func (f *fakeService) ListFiles(_ context.Context, _ uuid.UUID) ([]model.SourceFile, error) {
	return f.files, f.filesErr
}

func (f *fakeService) OpenFile(_ context.Context, _ uuid.UUID, _ string) (io.ReadCloser, string, error) {
	if f.openErr != nil {
		return nil, "", f.openErr
	}
	return io.NopCloser(strings.NewReader(f.fileContent)), f.fileType, nil
}

// fakeMembers is a test double for workspaces.Service controlling whether the
// caller is treated as a member of the request's workspace.
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

// allowMembers returns a members double that treats every caller as a member.
func allowMembers() fakeMembers { return fakeMembers{member: true} }

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
	_ = mw.Close()

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
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), allowMembers(), testMaxUpload)

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

func TestCreateSource_UploadTooLarge(t *testing.T) {
	// A tiny max makes the multipart body exceed the limit, so ParseMultipartForm
	// fails on the MaxBytesReader and the handler responds 400.
	h := NewHandler(&fakeService{source: sourceWithStatus(model.SourceStatusIndexing)}, NewProgressBus(), allowMembers(), 10)

	r := multipartUpload(map[string]string{
		"workspace_id": "11111111-1111-1111-1111-111111111111",
		"name":         "My Vault",
	}, "notes.md", strings.Repeat("x", 1024))
	w := httptest.NewRecorder()
	h.CreateSource(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized upload, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateSource_ForbiddenWhenNotMember(t *testing.T) {
	h := NewHandler(&fakeService{source: sourceWithStatus(model.SourceStatusIndexing)}, NewProgressBus(), fakeMembers{member: false}, testMaxUpload)

	r := multipartUpload(map[string]string{
		"workspace_id": "11111111-1111-1111-1111-111111111111",
		"name":         "Someone else's vault",
	}, "notes.md", "# Hello")
	w := httptest.NewRecorder()
	h.CreateSource(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListSources_ForbiddenWhenNotMember(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus(), fakeMembers{member: false}, testMaxUpload)

	r := httptest.NewRequest(http.MethodGet, "/sources?workspace_id=11111111-1111-1111-1111-111111111111", nil)
	w := httptest.NewRecorder()
	h.ListSources(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateSource_InvalidWorkspaceID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus(), allowMembers(), testMaxUpload)

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
	h := NewHandler(&fakeService{}, NewProgressBus(), allowMembers(), testMaxUpload)

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
	h := NewHandler(&fakeService{}, NewProgressBus(), allowMembers(), testMaxUpload)

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
	h := NewHandler(&fakeService{err: errTest("create failed")}, NewProgressBus(), allowMembers(), testMaxUpload)

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
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), allowMembers(), testMaxUpload)

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
	h := NewHandler(&fakeService{}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := httptest.NewRequest(http.MethodGet, "/sources?workspace_id=bad", nil)
	w := httptest.NewRecorder()
	h.ListSources(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListSources_ServiceError(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("db error")}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := httptest.NewRequest(http.MethodGet, "/sources?workspace_id=11111111-1111-1111-1111-111111111111", nil)
	w := httptest.NewRecorder()
	h.ListSources(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── IngestSource ─────────────────────────────────────────────────────────────

func TestIngestSource_Success(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodPost, "/sources/"+src.ID.String()+"/ingest",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.IngestSource(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIngestSource_ForbiddenWhenNotMember(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), fakeMembers{member: false}, testMaxUpload)

	r := routedRequest(http.MethodPost, "/sources/"+src.ID.String()+"/ingest",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.IngestSource(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestIngestSource_SourceNotFound(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("not found")}, NewProgressBus(), allowMembers(), testMaxUpload)
	id := uuid.New()

	r := routedRequest(http.MethodPost, "/sources/"+id.String()+"/ingest",
		map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.IngestSource(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestIngestSource_InvalidID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodPost, "/sources/bad/ingest", map[string]string{"id": "bad"}, nil)
	w := httptest.NewRecorder()
	h.IngestSource(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestIngestSource_ServiceError(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src, ingestErr: errTest("ingest error")}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodPost, "/sources/"+src.ID.String()+"/ingest",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.IngestSource(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestIngestSource_AlreadyIndexing(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexing)
	h := NewHandler(&fakeService{source: src, ingestErr: ErrAlreadyIndexing}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodPost, "/sources/"+src.ID.String()+"/ingest",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.IngestSource(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

// ── DeleteSource ─────────────────────────────────────────────────────────────

func TestDeleteSource_Success(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodDelete, "/sources/"+src.ID.String(),
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.DeleteSource(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteSource_InvalidID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodDelete, "/sources/bad", map[string]string{"id": "bad"}, nil)
	w := httptest.NewRecorder()
	h.DeleteSource(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteSource_ForbiddenWhenNotMember(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), fakeMembers{member: false}, testMaxUpload)

	r := routedRequest(http.MethodDelete, "/sources/"+src.ID.String(),
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.DeleteSource(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDeleteSource_SourceNotFound(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("not found")}, NewProgressBus(), allowMembers(), testMaxUpload)
	id := uuid.New()

	r := routedRequest(http.MethodDelete, "/sources/"+id.String(), map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.DeleteSource(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestDeleteSource_ServiceError(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src, deleteErr: errTest("delete error")}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodDelete, "/sources/"+src.ID.String(),
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.DeleteSource(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── ListChunks ────────────────────────────────────────────────────────────────

func TestListChunks_Success(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	chunks := []model.Chunk{{ID: uuid.New(), Content: "hello"}}
	h := NewHandler(&fakeService{source: src, chunks: chunks}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/chunks",
		map[string]string{"id": src.ID.String()}, nil)
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

func TestListChunks_ForbiddenWhenNotMember(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), fakeMembers{member: false}, testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/chunks",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.ListChunks(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestListChunks_InvalidID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/bad/chunks", map[string]string{"id": "bad"}, nil)
	w := httptest.NewRecorder()
	h.ListChunks(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListChunks_ServiceError(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("db error")}, NewProgressBus(), allowMembers(), testMaxUpload)
	id := uuid.New()

	r := routedRequest(http.MethodGet, "/sources/"+id.String()+"/chunks",
		map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.ListChunks(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── ListFiles ─────────────────────────────────────────────────────────────────

func TestListFiles_Success(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	files := []model.SourceFile{{ID: uuid.New(), SourceID: src.ID, Name: "docs/intro.md", Size: 42, ContentType: "text/markdown"}}
	h := NewHandler(&fakeService{source: src, files: files}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/files",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.ListFiles(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got []model.SourceFile
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Name != "docs/intro.md" {
		t.Fatalf("unexpected files: %+v", got)
	}
}

func TestListFiles_ForbiddenWhenNotMember(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), fakeMembers{member: false}, testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/files",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.ListFiles(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestListFiles_InvalidID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/bad/files", map[string]string{"id": "bad"}, nil)
	w := httptest.NewRecorder()
	h.ListFiles(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListFiles_SourceNotFound(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("not found")}, NewProgressBus(), allowMembers(), testMaxUpload)
	id := uuid.New()

	r := routedRequest(http.MethodGet, "/sources/"+id.String()+"/files",
		map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.ListFiles(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListFiles_ServiceError(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src, filesErr: errTest("db error")}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/files",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.ListFiles(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListFiles_EmptyList(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/files",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.ListFiles(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "[]\n" {
		t.Errorf("expected empty JSON array, got %q", got)
	}
}

// ── GetFileContent ────────────────────────────────────────────────────────────

func TestGetFileContent_Success(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src, fileContent: "# Hello", fileType: "text/markdown"}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/files/content?path=docs/intro.md",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.GetFileContent(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/markdown" {
		t.Errorf("expected content-type text/markdown, got %q", ct)
	}
	if w.Body.String() != "# Hello" {
		t.Errorf("unexpected body: %q", w.Body.String())
	}
}

func TestGetFileContent_MissingPath(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/files/content",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.GetFileContent(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetFileContent_ForbiddenWhenNotMember(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), fakeMembers{member: false}, testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/files/content?path=x.md",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.GetFileContent(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestGetFileContent_InvalidID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/bad/files/content?path=x.md",
		map[string]string{"id": "bad"}, nil)
	w := httptest.NewRecorder()
	h.GetFileContent(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetFileContent_SourceNotFound(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("not found")}, NewProgressBus(), allowMembers(), testMaxUpload)
	id := uuid.New()

	r := routedRequest(http.MethodGet, "/sources/"+id.String()+"/files/content?path=x.md",
		map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.GetFileContent(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetFileContent_FileNotFound(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src, openErr: errTest("missing")}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/files/content?path=x.md",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.GetFileContent(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetFileContent_DefaultsContentType(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	// Empty content type from the service falls back to octet-stream.
	h := NewHandler(&fakeService{source: src, fileContent: "raw", fileType: ""}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/files/content?path=blob.bin",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.GetFileContent(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("expected octet-stream fallback, got %q", ct)
	}
	// Unknown types are served as attachments so opening the raw URL downloads
	// rather than letting the browser sniff and render them.
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("expected attachment disposition for octet-stream, got %q", cd)
	}
	if xcto := w.Header().Get("X-Content-Type-Options"); xcto != "nosniff" {
		t.Errorf("expected nosniff, got %q", xcto)
	}
}

// getFileContent drives GetFileContent for a file served with the given content
// type and returns the recorder for header assertions.
func getFileContent(t *testing.T, fileType string) *httptest.ResponseRecorder {
	t.Helper()
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src, fileContent: "data", fileType: fileType}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/files/content?path=file",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.GetFileContent(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	return w
}

func TestGetFileContent_Disposition(t *testing.T) {
	// Active document types that can execute script on the app origin must be
	// served as attachments; only non-SVG images and PDFs stay inline so the
	// frontend's <img>/<iframe> previews keep working. SVG is an image but can
	// carry inline scripts, so it is treated as unsafe.
	cases := []struct {
		name        string
		contentType string
		want        string
	}{
		{"html", "text/html", "attachment"},
		{"html with charset", "text/html; charset=utf-8", "attachment"},
		{"svg", "image/svg+xml", "attachment"},
		{"xhtml", "application/xhtml+xml", "attachment"},
		{"unparseable", "image/png; charset", "attachment"}, // ParseMediaType error → unsafe
		{"png", "image/png", "inline"},
		{"pdf", "application/pdf", "inline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := getFileContent(t, tc.contentType)
			if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, tc.want) {
				t.Errorf("content type %q: expected %q disposition, got %q", tc.contentType, tc.want, cd)
			}
			// nosniff must be set regardless of disposition so the browser can't
			// sniff the declared type into something executable.
			if xcto := w.Header().Get("X-Content-Type-Options"); xcto != "nosniff" {
				t.Errorf("content type %q: expected nosniff, got %q", tc.contentType, xcto)
			}
		})
	}
}

// ── StreamStatus ─────────────────────────────────────────────────────────────

func TestStreamStatus_InvalidID(t *testing.T) {
	h := NewHandler(&fakeService{}, NewProgressBus(), allowMembers(), testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/bad/status", map[string]string{"id": "bad"}, nil)
	w := httptest.NewRecorder()
	h.StreamStatus(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStreamStatus_SourceNotFound(t *testing.T) {
	h := NewHandler(&fakeService{err: errTest("not found")}, NewProgressBus(), allowMembers(), testMaxUpload)
	id := uuid.New()

	r := routedRequest(http.MethodGet, "/sources/"+id.String()+"/status",
		map[string]string{"id": id.String()}, nil)
	w := httptest.NewRecorder()
	h.StreamStatus(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStreamStatus_ForbiddenWhenNotMember(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexing)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), fakeMembers{member: false}, testMaxUpload)

	r := routedRequest(http.MethodGet, "/sources/"+src.ID.String()+"/status",
		map[string]string{"id": src.ID.String()}, nil)
	w := httptest.NewRecorder()
	h.StreamStatus(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected no SSE headers on forbidden response, got Content-Type %q", ct)
	}
}

func TestStreamStatus_AlreadyIndexed(t *testing.T) {
	src := sourceWithStatus(model.SourceStatusIndexed)
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), allowMembers(), testMaxUpload)

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
	h := NewHandler(&fakeService{source: src}, NewProgressBus(), allowMembers(), testMaxUpload)

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
	h := NewHandler(&fakeService{source: src}, bus, allowMembers(), testMaxUpload)

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
	defer resp.Body.Close() //nolint:errcheck

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
