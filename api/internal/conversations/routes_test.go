package conversations

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/model"
)

// TestRoutes confirms all three endpoints are mounted on the subrouter. Auth
// is applied at the mount point in router.go, so it is out of scope here.
func TestRoutes(t *testing.T) {
	wsID := uuid.New()
	convID := uuid.New()
	svc := &fakeService{
		created:  &model.Conversation{ID: convID, WorkspaceID: wsID},
		list:     []model.Conversation{{ID: convID, WorkspaceID: wsID}},
		conv:     &model.Conversation{ID: convID, WorkspaceID: wsID},
		messages: []model.Message{},
	}
	r := Routes(NewHandler(svc, allowMembers()))

	cases := []struct {
		name       string
		method     string
		target     string
		body       string
		wantStatus int
	}{
		{name: "create", method: http.MethodPost, target: "/", body: `{"workspace_id":"` + wsID.String() + `"}`, wantStatus: http.StatusCreated},
		{name: "list", method: http.MethodGet, target: "/?workspace_id=" + wsID.String(), wantStatus: http.StatusOK},
		{name: "list messages", method: http.MethodGet, target: "/" + convID.String() + "/messages", wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.target, body)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}
