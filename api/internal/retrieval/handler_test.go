package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/conversations"
	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

// fakeRetriever is a minimal test double for Retriever.
type fakeRetriever struct {
	results   []RetrievedChunk
	err       error
	gotReq    RetrieveRequest
	streamOut []llm.StreamChunk // chunks Answer emits on its channel
	answerErr error
}

func (f *fakeRetriever) Retrieve(_ context.Context, req RetrieveRequest) ([]RetrievedChunk, error) {
	f.gotReq = req
	return f.results, f.err
}

func (f *fakeRetriever) Answer(_ context.Context, req RetrieveRequest) ([]RetrievedChunk, <-chan llm.StreamChunk, error) {
	f.gotReq = req
	if f.answerErr != nil {
		return nil, nil, f.answerErr
	}
	ch := make(chan llm.StreamChunk, len(f.streamOut))
	for _, c := range f.streamOut {
		ch <- c
	}
	close(ch)
	return f.results, ch, nil
}

type errTest string

func (e errTest) Error() string { return string(e) }

// fakeMembers is a test double for workspaces.Service controlling whether the
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

// allowMembers returns a members double that treats every caller as a member.
func allowMembers() fakeMembers { return fakeMembers{member: true} }

// appendedMessage records one call to fakeConversationService.AppendMessage.
type appendedMessage struct {
	conversationID uuid.UUID
	role           model.MessageRole
	content        string
	sources        json.RawMessage
}

// fakeConversationService is a conversations.Service test double.
type fakeConversationService struct {
	conv      *model.Conversation
	getErr    error
	appendErr error
	appended  []appendedMessage
}

func (f *fakeConversationService) Create(context.Context, uuid.UUID) (*model.Conversation, error) {
	return nil, nil
}
func (f *fakeConversationService) List(context.Context, uuid.UUID) ([]model.Conversation, error) {
	return nil, nil
}
func (f *fakeConversationService) GetByID(_ context.Context, id uuid.UUID) (*model.Conversation, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.conv != nil {
		return f.conv, nil
	}
	return &model.Conversation{ID: id}, nil
}
func (f *fakeConversationService) ListMessages(context.Context, uuid.UUID) ([]model.Message, error) {
	return nil, nil
}
func (f *fakeConversationService) AppendMessage(_ context.Context, conversationID uuid.UUID, role model.MessageRole, content string, sources json.RawMessage) (*model.Message, error) {
	if f.appendErr != nil {
		return nil, f.appendErr
	}
	f.appended = append(f.appended, appendedMessage{conversationID, role, content, sources})
	return &model.Message{ID: uuid.New(), ConversationID: conversationID, Role: role, Content: content, Sources: sources}, nil
}

// noConversations returns a conversations double for tests that never pass a
// conversation_id, so h.conversations is never actually invoked.
func noConversations() *fakeConversationService { return &fakeConversationService{} }

func postQuery(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/query", bytes.NewBufferString(body))
}

func TestQuery_Success(t *testing.T) {
	wid := uuid.New()
	chunkID := uuid.New()
	fake := &fakeRetriever{results: []RetrievedChunk{
		{Chunk: model.Chunk{ID: chunkID, Content: "hello world"}, Score: 0.87},
	}}
	h := NewHandler(fake, allowMembers(), noConversations())

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

func TestQueryStream_EmitsSourcesTokensDone(t *testing.T) {
	wid := uuid.New()
	chunkID := uuid.New()
	fake := &fakeRetriever{
		results:   []RetrievedChunk{{Chunk: model.Chunk{ID: chunkID, Content: "grounding"}, Score: 0.9}},
		streamOut: []llm.StreamChunk{{Content: "Hel"}, {Content: "lo"}, {Done: true}},
	}
	h := NewHandler(fake, allowMembers(), noConversations())

	body := `{"workspace_id":"` + wid.String() + `","question":"hi?"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected SSE content-type, got %q", ct)
	}
	out := w.Body.String()
	for _, want := range []string{"event: sources", chunkID.String(), "event: token", `"Hel"`, `"lo"`, "event: done"} {
		if !strings.Contains(out, want) {
			t.Errorf("stream missing %q\nfull body:\n%s", want, out)
		}
	}
}

func TestQueryStream_ForbiddenWhenNotMember(t *testing.T) {
	fake := &fakeRetriever{}
	h := NewHandler(fake, fakeMembers{member: false}, noConversations())

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi?"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if fake.gotReq.WorkspaceID != uuid.Nil {
		t.Errorf("Answer was called despite forbidden access")
	}
}

func TestQuery_ForbiddenWhenNotMember(t *testing.T) {
	wid := uuid.New()
	fake := &fakeRetriever{}
	h := NewHandler(fake, fakeMembers{member: false}, noConversations())

	body := `{"workspace_id":"` + wid.String() + `","question":"secret?"}`
	w := httptest.NewRecorder()
	h.Query(w, postQuery(body))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	// The retriever must never run for a non-member: no cross-workspace data.
	if fake.gotReq.WorkspaceID != uuid.Nil {
		t.Errorf("retriever was called despite forbidden access: %s", fake.gotReq.WorkspaceID)
	}
}

func TestQuery_InvalidBody(t *testing.T) {
	h := NewHandler(&fakeRetriever{}, allowMembers(), noConversations())

	w := httptest.NewRecorder()
	h.Query(w, postQuery("not json"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQuery_MissingWorkspaceID(t *testing.T) {
	h := NewHandler(&fakeRetriever{}, allowMembers(), noConversations())

	w := httptest.NewRecorder()
	h.Query(w, postQuery(`{"question":"hi"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQuery_MissingQuestion(t *testing.T) {
	h := NewHandler(&fakeRetriever{}, allowMembers(), noConversations())

	w := httptest.NewRecorder()
	h.Query(w, postQuery(`{"workspace_id":"`+uuid.New().String()+`"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQuery_ServiceError(t *testing.T) {
	h := NewHandler(&fakeRetriever{err: errTest("boom")}, allowMembers(), noConversations())

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi"}`
	w := httptest.NewRecorder()
	h.Query(w, postQuery(body))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestQuery_EmptyResults(t *testing.T) {
	h := NewHandler(&fakeRetriever{results: []RetrievedChunk{}}, allowMembers(), noConversations())

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

func TestQuery_ConversationOmitted_NoPersistence(t *testing.T) {
	convSvc := noConversations()
	h := NewHandler(&fakeRetriever{}, allowMembers(), convSvc)

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi"}`
	w := httptest.NewRecorder()
	h.Query(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(convSvc.appended) != 0 {
		t.Fatalf("expected no persistence when conversation_id is omitted, got %d appended messages", len(convSvc.appended))
	}
}

func TestQuery_PersistsQuestionOnly(t *testing.T) {
	wid := uuid.New()
	convID := uuid.New()
	convSvc := &fakeConversationService{conv: &model.Conversation{ID: convID, WorkspaceID: wid}}
	h := NewHandler(&fakeRetriever{}, allowMembers(), convSvc)

	body := `{"workspace_id":"` + wid.String() + `","question":"how does it work?","conversation_id":"` + convID.String() + `"}`
	w := httptest.NewRecorder()
	h.Query(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// /query never generates an answer, so only the question is persisted.
	if len(convSvc.appended) != 1 {
		t.Fatalf("expected exactly 1 persisted message, got %d", len(convSvc.appended))
	}
	got := convSvc.appended[0]
	if got.role != model.MessageRoleUser || got.content != "how does it work?" {
		t.Errorf("unexpected persisted message: %+v", got)
	}
}

func TestQuery_ConversationWorkspaceMismatch(t *testing.T) {
	convID := uuid.New()
	convSvc := &fakeConversationService{conv: &model.Conversation{ID: convID, WorkspaceID: uuid.New()}}
	h := NewHandler(&fakeRetriever{}, allowMembers(), convSvc)

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi","conversation_id":"` + convID.String() + `"}`
	w := httptest.NewRecorder()
	h.Query(w, postQuery(body))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if len(convSvc.appended) != 0 {
		t.Fatalf("expected no persistence on workspace mismatch, got %d", len(convSvc.appended))
	}
}

func TestQuery_ConversationNotFound(t *testing.T) {
	convSvc := &fakeConversationService{getErr: conversations.ErrNotFound}
	h := NewHandler(&fakeRetriever{}, allowMembers(), convSvc)

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi","conversation_id":"` + uuid.New().String() + `"}`
	w := httptest.NewRecorder()
	h.Query(w, postQuery(body))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
