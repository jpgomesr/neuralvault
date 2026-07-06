package workspaces

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/model"
)

// TestRoutes confirms both endpoints are mounted on the subrouter. Auth is
// applied at the mount point in router.go, so it is out of scope here.
func TestRoutes(t *testing.T) {
	svc := &fakeService{
		created: &model.Workspace{ID: uuid.New(), Name: "WS"},
		list:    []model.Workspace{},
	}
	r := Routes(NewHandler(svc))

	cases := []struct {
		name       string
		method     string
		body       string
		wantStatus int
	}{
		{name: "create", method: http.MethodPost, body: `{"name":"WS"}`, wantStatus: http.StatusCreated},
		{name: "list", method: http.MethodGet, wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, "/", body)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}
