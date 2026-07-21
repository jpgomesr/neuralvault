package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jpgomesr/neuralvault/api/internal/model"
	"github.com/jpgomesr/neuralvault/api/internal/workspaces"
)

// fakeService is a Service stand-in for exercising the HTTP layer without a
// database.
type fakeService struct {
	created    *model.Conversation
	createErr  error
	list       []model.Conversation
	listErr    error
	conv       *model.Conversation
	getErr     error
	messages   []model.Message
	messageErr error
}

func (f *fakeService) Create(_ context.Context, workspaceID uuid.UUID) (*model.Conversation, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.created != nil {
		return f.created, nil
	}
	return &model.Conversation{ID: uuid.New(), WorkspaceID: workspaceID}, nil
}

func (f *fakeService) List(_ context.Context, _ uuid.UUID) ([]model.Conversation, error) {
	return f.list, f.listErr
}

func (f *fakeService) GetByID(_ context.Context, _ uuid.UUID) (*model.Conversation, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.conv, nil
}

func (f *fakeService) ListMessages(_ context.Context, _ uuid.UUID) ([]model.Message, error) {
	return f.messages, f.messageErr
}

func (f *fakeService) AppendMessage(_ context.Context, conversationID uuid.UUID, role model.MessageRole, content string, sources json.RawMessage) (*model.Message, error) {
	return &model.Message{ID: uuid.New(), ConversationID: conversationID, Role: role, Content: content, Sources: sources}, nil
}

// fakeMembers is a workspaces.Service stand-in that treats every caller as a
// member (or not), ignoring which user/workspace is asked about.
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
func denyMembers() fakeMembers  { return fakeMembers{member: false} }

var _ workspaces.Service = fakeMembers{}

// routedRequest attaches Chi URL params to an httptest request.
func routedRequest(method, target string, params map[string]string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestHandlerCreate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		wsID := uuid.New()
		conv := &model.Conversation{ID: uuid.New(), WorkspaceID: wsID}
		h := NewHandler(&fakeService{created: conv}, allowMembers())

		req := httptest.NewRequest(http.MethodPost, "/conversations", strings.NewReader(`{"workspace_id":"`+wsID.String()+`"}`))
		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusCreated)
		}
		var body model.Conversation
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		if body.ID != conv.ID {
			t.Fatalf("body id: got %s, want %s", body.ID, conv.ID)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		h := NewHandler(&fakeService{}, allowMembers())
		req := httptest.NewRequest(http.MethodPost, "/conversations", strings.NewReader("{"))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("missing workspace_id", func(t *testing.T) {
		h := NewHandler(&fakeService{}, allowMembers())
		req := httptest.NewRequest(http.MethodPost, "/conversations", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("not a member", func(t *testing.T) {
		h := NewHandler(&fakeService{}, denyMembers())
		req := httptest.NewRequest(http.MethodPost, "/conversations", strings.NewReader(`{"workspace_id":"`+uuid.New().String()+`"}`))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := NewHandler(&fakeService{createErr: errors.New("boom")}, allowMembers())
		req := httptest.NewRequest(http.MethodPost, "/conversations", strings.NewReader(`{"workspace_id":"`+uuid.New().String()+`"}`))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestHandlerList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		want := []model.Conversation{{ID: uuid.New()}, {ID: uuid.New()}}
		h := NewHandler(&fakeService{list: want}, allowMembers())

		req := httptest.NewRequest(http.MethodGet, "/conversations?workspace_id="+uuid.New().String(), nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
		}
		var got []model.Conversation
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		if len(got) != len(want) {
			t.Fatalf("len: got %d, want %d", len(got), len(want))
		}
	})

	t.Run("nil list serializes as empty array", func(t *testing.T) {
		h := NewHandler(&fakeService{list: nil}, allowMembers())
		req := httptest.NewRequest(http.MethodGet, "/conversations?workspace_id="+uuid.New().String(), nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
			t.Fatalf("body: got %q, want %q", body, "[]")
		}
	})

	t.Run("invalid workspace_id", func(t *testing.T) {
		h := NewHandler(&fakeService{}, allowMembers())
		req := httptest.NewRequest(http.MethodGet, "/conversations?workspace_id=bad", nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("not a member", func(t *testing.T) {
		h := NewHandler(&fakeService{}, denyMembers())
		req := httptest.NewRequest(http.MethodGet, "/conversations?workspace_id="+uuid.New().String(), nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := NewHandler(&fakeService{listErr: errors.New("boom")}, allowMembers())
		req := httptest.NewRequest(http.MethodGet, "/conversations?workspace_id="+uuid.New().String(), nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestHandlerListMessages(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		wsID := uuid.New()
		convID := uuid.New()
		want := []model.Message{{ID: uuid.New(), Role: model.MessageRoleUser}, {ID: uuid.New(), Role: model.MessageRoleAssistant}}
		h := NewHandler(&fakeService{conv: &model.Conversation{ID: convID, WorkspaceID: wsID}, messages: want}, allowMembers())

		req := routedRequest(http.MethodGet, "/conversations/"+convID.String()+"/messages", map[string]string{"id": convID.String()})
		rec := httptest.NewRecorder()
		h.ListMessages(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
		}
		var got []model.Message
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		if len(got) != len(want) {
			t.Fatalf("len: got %d, want %d", len(got), len(want))
		}
	})

	t.Run("nil messages serializes as empty array", func(t *testing.T) {
		convID := uuid.New()
		h := NewHandler(&fakeService{conv: &model.Conversation{ID: convID}, messages: nil}, allowMembers())

		req := routedRequest(http.MethodGet, "/conversations/"+convID.String()+"/messages", map[string]string{"id": convID.String()})
		rec := httptest.NewRecorder()
		h.ListMessages(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
			t.Fatalf("body: got %q, want %q", body, "[]")
		}
	})

	t.Run("invalid id", func(t *testing.T) {
		h := NewHandler(&fakeService{}, allowMembers())
		req := routedRequest(http.MethodGet, "/conversations/bad/messages", map[string]string{"id": "bad"})
		rec := httptest.NewRecorder()
		h.ListMessages(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("conversation not found", func(t *testing.T) {
		id := uuid.New()
		h := NewHandler(&fakeService{getErr: ErrNotFound}, allowMembers())
		req := routedRequest(http.MethodGet, "/conversations/"+id.String()+"/messages", map[string]string{"id": id.String()})
		rec := httptest.NewRecorder()
		h.ListMessages(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("get error", func(t *testing.T) {
		id := uuid.New()
		h := NewHandler(&fakeService{getErr: errors.New("boom")}, allowMembers())
		req := routedRequest(http.MethodGet, "/conversations/"+id.String()+"/messages", map[string]string{"id": id.String()})
		rec := httptest.NewRecorder()
		h.ListMessages(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
		}
	})

	t.Run("not a member", func(t *testing.T) {
		id := uuid.New()
		h := NewHandler(&fakeService{conv: &model.Conversation{ID: id}}, denyMembers())
		req := routedRequest(http.MethodGet, "/conversations/"+id.String()+"/messages", map[string]string{"id": id.String()})
		rec := httptest.NewRecorder()
		h.ListMessages(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("list messages error", func(t *testing.T) {
		id := uuid.New()
		h := NewHandler(&fakeService{conv: &model.Conversation{ID: id}, messageErr: errors.New("boom")}, allowMembers())
		req := routedRequest(http.MethodGet, "/conversations/"+id.String()+"/messages", map[string]string{"id": id.String()})
		rec := httptest.NewRecorder()
		h.ListMessages(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
		}
	})
}
