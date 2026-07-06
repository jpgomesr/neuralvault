package retrieval

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/llm"
)

// These cover the QueryStream branches the happy-path/forbidden tests don't
// reach: request validation, an Answer failure before the stream opens, and an
// error chunk arriving mid-stream.

func TestQueryStream_InvalidBody(t *testing.T) {
	h := NewHandler(&fakeRetriever{}, allowMembers())

	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery("not json"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQueryStream_MissingWorkspaceID(t *testing.T) {
	h := NewHandler(&fakeRetriever{}, allowMembers())

	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(`{"question":"hi"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQueryStream_MissingQuestion(t *testing.T) {
	h := NewHandler(&fakeRetriever{}, allowMembers())

	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(`{"workspace_id":"`+uuid.New().String()+`"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQueryStream_AnswerError(t *testing.T) {
	h := NewHandler(&fakeRetriever{answerErr: errTest("boom")}, allowMembers())

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestQueryStream_EmitsErrorEvent(t *testing.T) {
	fake := &fakeRetriever{
		streamOut: []llm.StreamChunk{{Content: "partial"}, {Error: errTest("model exploded")}},
	}
	h := NewHandler(fake, allowMembers())

	body := `{"workspace_id":"` + uuid.New().String() + `","question":"hi?"}`
	w := httptest.NewRecorder()
	h.QueryStream(w, postQuery(body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (headers already sent), got %d", w.Code)
	}
	out := w.Body.String()
	if !strings.Contains(out, "event: error") || !strings.Contains(out, "model exploded") {
		t.Fatalf("expected an error event in the stream, got:\n%s", out)
	}
}
