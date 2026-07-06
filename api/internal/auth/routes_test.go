package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// TestRoutes exercises the mounted router so the public endpoints and the
// RequireUser-protected /me are wired as intended.
func TestRoutes(t *testing.T) {
	h := NewHandler(&fakeService{}, testSecret, false, "/app")
	r := Routes(h)

	validToken, err := h.signer.Issue(uuid.New(), "user@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	cases := []struct {
		name       string
		method     string
		path       string
		cookie     *http.Cookie
		wantStatus int
	}{
		{name: "login is public", method: http.MethodGet, path: "/login", wantStatus: http.StatusFound},
		{name: "logout is public", method: http.MethodPost, path: "/logout", wantStatus: http.StatusNoContent},
		{name: "me without session", method: http.MethodGet, path: "/me", wantStatus: http.StatusUnauthorized},
		{name: "me with session", method: http.MethodGet, path: "/me", cookie: &http.Cookie{Name: sessionCookie, Value: validToken}, wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}
