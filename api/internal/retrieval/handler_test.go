package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

// fakeRetriever is a minimal test double for Retriever.
type fakeRetriever struct {
	results []RetrievedChunk
	err     error
	gotReq  RetrieveRequest
}

func (f *fakeRetriever) Retrieve(_ context.Context, req RetrieveRequest) ([]RetrievedChunk, error) {
	f.gotReq = req
	return f.results, f.err
}

type errTest string

func (e errTest) Error() string { return string(e) }

func postQuery(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(body))
}

func TestQuery_Success(t *testing.T) {
	wid := uuid.New()
	chunkID := uuid.New()
	fake := &fakeRetriever{results: []RetrievedChunk{
		{Chunk: model.Chunk{ID: chunkID, Content: "hello world"}, Score: 0.87},
	}}
	h := NewHandler(fake)

	body := `{"workspace_id":"` + wid.String() + `","question":"how does it work?","top_k":3}`
	w := httptest.NewRecorder()
	h.Query(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp queryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].ChunkID != chunkID.String() {
		t.Errorf("chunk_id mismatch: want %s, got %s", chunkID, resp.Results[0].ChunkID)
	}
	if resp.Results[0].Content != "hello world" {
		t.Errorf("content mismatch: got %q", resp.Results[0].Content)
	}
	if resp.Results[0].Score != 0.87 {
		t.Errorf("score mismatch: got %v", resp.Results[0].Score)
	}

	if fake.gotReq.WorkspaceID != wid {
		t.Errorf("workspace_id not forwarded to service: got %s", fake.gotReq.WorkspaceID)
	}
	if fake.gotReq.Query != "how does it work?" {
		t.Errorf("question not forwarded to service as Query: got %q", fake.gotReq.Query)
	}
	if fake.gotReq.TopK != 3 {
		t.Errorf("top_k not forwarded to service: got %d", fake.gotReq.TopK)
	}
}

func TestQuery_InvalidBody(t *testing.T) {
	h := NewHandler(&fakeRetriever{})

	w := httptest.NewRecorder()
	h.Query(w, postQuery("not json"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQuery_MissingWorkspaceID(t *testing.T) {
	h := NewHandler(&fakeRetriever{})

	w := httptest.NewRecorder()
	h.Query(w, postQuery(`{"question":"hi"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQuery_MissingQuestion(t *testing.T) {
	h := NewHandler(&fakeRetriever{})

	w := httptest.NewRecorder()
	h.Query(w, postQuery(`{"workspace_id":"`+uuid.New().String()+`"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQuery_ServiceError(t *testing.T) {
	h := NewHandler(&fakeRetriever{err: errTest("boom")})

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi"}`
	w := httptest.NewRecorder()
	h.Query(w, postQuery(body))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestQuery_EmptyResults(t *testing.T) {
	h := NewHandler(&fakeRetriever{results: []RetrievedChunk{}})

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi"}`
	w := httptest.NewRecorder()
	h.Query(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp queryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected empty results, got %d", len(resp.Results))
	}
}
