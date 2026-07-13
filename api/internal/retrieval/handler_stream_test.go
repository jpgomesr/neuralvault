package retrieval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/llm"
	"github.com/jpgomesr/NeuralVault/internal/model"
)

// These cover the QueryStream branches the happy-path/forbidden tests don't
// reach: request validation, an Answer failure before the stream opens, and an
// error chunk arriving mid-stream.

func TestQueryStream_InvalidBody(t *testing.T) {
	h := NewHandler(&fakeRetriever{}, allowMembers(), noConversations())

	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery("not json"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQueryStream_MissingWorkspaceID(t *testing.T) {
	h := NewHandler(&fakeRetriever{}, allowMembers(), noConversations())

	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(`{"question":"hi"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQueryStream_MissingQuestion(t *testing.T) {
	h := NewHandler(&fakeRetriever{}, allowMembers(), noConversations())

	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(`{"workspace_id":"`+uuid.New().String()+`"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQueryStream_AnswerError(t *testing.T) {
	h := NewHandler(&fakeRetriever{answerErr: errTest("boom")}, allowMembers(), noConversations())

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	// SSE headers are sent before Answer() runs (see handler.go), so an
	// Answer() failure is reported as an SSE error event, not an HTTP error
	// status — the response is already committed to text/event-stream by the
	// time this failure is known.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (headers already sent), got %d", w.Code)
	}
	out := w.Body.String()
	if !strings.Contains(out, "event: error") {
		t.Fatalf("expected an error event in the stream, got:\n%s", out)
	}
	if strings.Contains(out, "boom") {
		t.Fatalf("error event leaked internal error detail:\n%s", out)
	}
}

// TestQueryStream_SendsHeartbeatDuringSlowAnswer proves the actual point of
// the heartbeat mechanism: while Answer() is slow (a cold-loading local LLM,
// in production), the SSE stream still emits bytes periodically so a proxy
// sitting in front of this API never sees enough silence to kill the
// connection.
func TestQueryStream_SendsHeartbeatDuringSlowAnswer(t *testing.T) {
	orig := sseHeartbeatInterval
	sseHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { sseHeartbeatInterval = orig })

	fake := &fakeRetriever{
		answerDelay: 50 * time.Millisecond,
		streamOut:   []llm.StreamChunk{{Done: true}},
	}
	h := NewHandler(fake, allowMembers(), noConversations())

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	out := w.Body.String()
	if !strings.Contains(out, ": keep-alive") {
		t.Fatalf("expected at least one heartbeat comment while Answer() was slow, got:\n%q", out)
	}
}

// TestQueryStream_SendsHeartbeatDuringSlowTokens proves the inter-token
// heartbeat: once the answer is streaming, a slow gap between generated tokens
// still emits periodic bytes so a proxy in front of this API never sees enough
// silence to kill the connection mid-stream.
func TestQueryStream_SendsHeartbeatDuringSlowTokens(t *testing.T) {
	orig := sseHeartbeatInterval
	sseHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { sseHeartbeatInterval = orig })

	fake := &fakeRetriever{
		streamOut:  []llm.StreamChunk{{Content: "tok"}, {Done: true}},
		tokenDelay: 50 * time.Millisecond,
	}
	h := NewHandler(fake, allowMembers(), noConversations())

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	out := w.Body.String()
	if !strings.Contains(out, ": keep-alive") {
		t.Fatalf("expected a heartbeat comment while tokens were slow, got:\n%q", out)
	}
	if !strings.Contains(out, "event: done") {
		t.Fatalf("expected the stream to still complete with a done event, got:\n%q", out)
	}
}

// TestQueryStream_ClientDisconnectDuringAnswer proves the handler stops waiting
// and returns when the client goes away (request context cancelled) while
// Answer() is still running, rather than blocking on a stream that will never
// be consumed.
func TestQueryStream_ClientDisconnectDuringAnswer(t *testing.T) {
	fake := &fakeRetriever{
		answerDelay: 200 * time.Millisecond,
		streamOut:   []llm.StreamChunk{{Done: true}},
	}
	h := NewHandler(fake, allowMembers(), noConversations())

	ctx, cancel := context.WithCancel(context.Background())
	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi"}`
	req := postQuery(body).WithContext(ctx)

	// Cancel well before Answer() would return, so the wait loop takes its
	// context-cancelled branch.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	w := httptest.NewRecorder()
	h.QueryStream(w, req)

	if strings.Contains(w.Body.String(), "event: done") {
		t.Fatalf("expected the handler to return on client disconnect without a done event, got:\n%q", w.Body.String())
	}
}

func TestQueryStream_EmitsErrorEvent(t *testing.T) {
	fake := &fakeRetriever{
		streamOut: []llm.StreamChunk{{Content: "partial"}, {Error: errTest("model exploded")}},
	}
	h := NewHandler(fake, allowMembers(), noConversations())

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi?"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (headers already sent), got %d", w.Code)
	}
	out := w.Body.String()
	if !strings.Contains(out, "event: error") {
		t.Fatalf("expected an error event in the stream, got:\n%s", out)
	}
	if strings.Contains(out, "model exploded") {
		t.Fatalf("error event leaked internal error detail:\n%s", out)
	}
}

func TestQueryStream_PersistsQuestionAndAnswer(t *testing.T) {
	wid := uuid.New()
	convID := uuid.New()
	chunkID := uuid.New()
	fake := &fakeRetriever{
		results:   []RetrievedChunk{{Chunk: model.Chunk{ID: chunkID, Content: "grounding"}, Score: 0.9}},
		streamOut: []llm.StreamChunk{{Content: "Hel"}, {Content: "lo"}, {Done: true}},
	}
	convSvc := &fakeConversationService{conv: &model.Conversation{ID: convID, WorkspaceID: wid}}
	h := NewHandler(fake, allowMembers(), convSvc)

	body := `{"workspace_id":"` + wid.String() + `","question":"hi?","conversation_id":"` + convID.String() + `"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(convSvc.appended) != 2 {
		t.Fatalf("expected 2 persisted messages (question + answer), got %d", len(convSvc.appended))
	}
	question, answer := convSvc.appended[0], convSvc.appended[1]
	if question.role != model.MessageRoleUser || question.content != "hi?" {
		t.Errorf("unexpected question message: %+v", question)
	}
	if answer.role != model.MessageRoleAssistant || answer.content != "Hello" {
		t.Errorf("unexpected answer message: %+v", answer)
	}
	if !strings.Contains(string(answer.sources), chunkID.String()) {
		t.Errorf("expected answer sources to carry the grounding chunk, got %s", answer.sources)
	}
}

func TestQueryStream_DoesNotPersistPartialAnswerOnError(t *testing.T) {
	wid := uuid.New()
	convID := uuid.New()
	fake := &fakeRetriever{
		streamOut: []llm.StreamChunk{{Content: "partial"}, {Error: errTest("model exploded")}},
	}
	convSvc := &fakeConversationService{conv: &model.Conversation{ID: convID, WorkspaceID: wid}}
	h := NewHandler(fake, allowMembers(), convSvc)

	body := `{"workspace_id":"` + wid.String() + `","question":"hi?","conversation_id":"` + convID.String() + `"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if len(convSvc.appended) != 1 {
		t.Fatalf("expected only the question persisted (no partial answer), got %d messages", len(convSvc.appended))
	}
	if convSvc.appended[0].role != model.MessageRoleUser {
		t.Errorf("expected the persisted message to be the question, got %+v", convSvc.appended[0])
	}
}

func TestQueryStream_PersistQuestionError(t *testing.T) {
	wid := uuid.New()
	convID := uuid.New()
	fake := &fakeRetriever{streamOut: []llm.StreamChunk{{Content: "hi"}, {Done: true}}}
	convSvc := &fakeConversationService{
		conv:      &model.Conversation{ID: convID, WorkspaceID: wid},
		appendErr: errTest("insert failed"),
	}
	h := NewHandler(fake, allowMembers(), convSvc)

	body := `{"workspace_id":"` + wid.String() + `","question":"hi?","conversation_id":"` + convID.String() + `"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	// SSE headers are sent before persisting the question (see handler.go),
	// so this failure is reported as an SSE error event, not an HTTP error
	// status.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (headers already sent), got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected SSE headers to already be written, got Content-Type %q", ct)
	}
	out := w.Body.String()
	if !strings.Contains(out, "event: error") {
		t.Fatalf("expected an error event in the stream, got:\n%s", out)
	}
	if strings.Contains(out, "insert failed") {
		t.Fatalf("error event leaked internal error detail:\n%s", out)
	}
}

func TestQueryStream_PersistAnswerError_LogsButDoesNotFailStream(t *testing.T) {
	wid := uuid.New()
	convID := uuid.New()
	fake := &fakeRetriever{streamOut: []llm.StreamChunk{{Content: "hi"}, {Done: true}}}
	// The question (1st AppendMessage call) persists fine; only the answer
	// (2nd call) fails — this happens after SSE headers are already flushed,
	// so the failure can only be logged, not turned into an HTTP error.
	convSvc := &fakeConversationService{
		conv:             &model.Conversation{ID: convID, WorkspaceID: wid},
		appendErr:        errTest("insert failed"),
		failAppendOnCall: 2,
	}
	h := NewHandler(fake, allowMembers(), convSvc)

	body := `{"workspace_id":"` + wid.String() + `","question":"hi?","conversation_id":"` + convID.String() + `"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (headers already sent), got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "event: done") {
		t.Fatalf("expected the stream to still complete with a done event, got:\n%s", w.Body.String())
	}
	if len(convSvc.appended) != 1 {
		t.Fatalf("expected only the question persisted, got %d messages", len(convSvc.appended))
	}
}

func TestQueryStream_ConversationWorkspaceMismatch(t *testing.T) {
	convID := uuid.New()
	convSvc := &fakeConversationService{conv: &model.Conversation{ID: convID, WorkspaceID: uuid.New()}}
	fake := &fakeRetriever{}
	h := NewHandler(fake, allowMembers(), convSvc)

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi?","conversation_id":"` + convID.String() + `"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if fake.gotReq.WorkspaceID != uuid.Nil {
		t.Errorf("Answer was called despite the conversation/workspace mismatch")
	}
}
