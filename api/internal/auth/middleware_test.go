package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestRequireUser(t *testing.T) {
	h := &Handler{signer: newSessionSigner(testSecret)}
	userID := uuid.New()
	validToken, err := h.signer.Issue(userID, "user@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// next records whether it ran and what principal it saw.
	var sawUser uuid.UUID
	ran := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ran = true
		sawUser = UserID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name       string
		cookie     *http.Cookie
		wantStatus int
		wantNext   bool
	}{
		{name: "no cookie", cookie: nil, wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "invalid cookie", cookie: &http.Cookie{Name: sessionCookie, Value: "garbage"}, wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "valid cookie", cookie: &http.Cookie{Name: sessionCookie, Value: validToken}, wantStatus: http.StatusOK, wantNext: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran, sawUser = false, uuid.Nil

			req := httptest.NewRequest(http.MethodGet, "/query", nil)
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			rec := httptest.NewRecorder()

			h.RequireUser(next).ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
			if ran != tc.wantNext {
				t.Fatalf("next ran: got %v, want %v", ran, tc.wantNext)
			}
			if tc.wantNext && sawUser != userID {
				t.Fatalf("context user: got %s, want %s", sawUser, userID)
			}
		})
	}
}
