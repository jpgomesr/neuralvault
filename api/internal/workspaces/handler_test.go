package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/model"
)

// fakeService is a Service stand-in for exercising the HTTP layer without a
// database.
type fakeService struct {
	created   *model.Workspace
	createErr error
	list      []model.Workspace
	listErr   error
	member    bool
	memberErr error

	gotName string
}

func (f *fakeService) Create(_ context.Context, _ uuid.UUID, name string) (*model.Workspace, error) {
	f.gotName = name
	return f.created, f.createErr
}

func (f *fakeService) List(_ context.Context, _ uuid.UUID) ([]model.Workspace, error) {
	return f.list, f.listErr
}

func (f *fakeService) IsMember(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return f.member, f.memberErr
}

func TestCreate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ws := &model.Workspace{ID: uuid.New(), Name: "My Workspace"}
		svc := &fakeService{created: ws}
		h := NewHandler(svc)

		req := httptest.NewRequest(http.MethodPost, "/workspaces", strings.NewReader(`{"name":"My Workspace"}`))
		rec := httptest.NewRecorder()
		h.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusCreated)
		}
		if svc.gotName != "My Workspace" {
			t.Fatalf("service saw name %q, want %q", svc.gotName, "My Workspace")
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type: got %q", ct)
		}
		var body model.Workspace
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		if body.ID != ws.ID {
			t.Fatalf("body id: got %s, want %s", body.ID, ws.ID)
		}
	})

	t.Run("rejections", func(t *testing.T) {
		cases := []struct {
			name       string
			body       string
			createErr  error
			wantStatus int
		}{
			{name: "invalid json", body: "{", wantStatus: http.StatusBadRequest},
			{name: "empty name", body: `{"name":""}`, wantStatus: http.StatusBadRequest},
			{name: "service error", body: `{"name":"ok"}`, createErr: errors.New("boom"), wantStatus: http.StatusInternalServerError},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				h := NewHandler(&fakeService{createErr: tc.createErr})
				req := httptest.NewRequest(http.MethodPost, "/workspaces", strings.NewReader(tc.body))
				rec := httptest.NewRecorder()
				h.Create(rec, req)

				if rec.Code != tc.wantStatus {
					t.Fatalf("status: got %d, want %d", rec.Code, tc.wantStatus)
				}
			})
		}
	})
}

func TestList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		want := []model.Workspace{{ID: uuid.New(), Name: "A"}, {ID: uuid.New(), Name: "B"}}
		h := NewHandler(&fakeService{list: want})

		req := httptest.NewRequest(http.MethodGet, "/workspaces", nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
		}
		var got []model.Workspace
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		if len(got) != len(want) {
			t.Fatalf("len: got %d, want %d", len(got), len(want))
		}
	})

	t.Run("nil list serializes as empty array", func(t *testing.T) {
		h := NewHandler(&fakeService{list: nil})

		req := httptest.NewRequest(http.MethodGet, "/workspaces", nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
			t.Fatalf("body: got %q, want %q", body, "[]")
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := NewHandler(&fakeService{listErr: errors.New("boom")})

		req := httptest.NewRequest(http.MethodGet, "/workspaces", nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
		}
	})
}
