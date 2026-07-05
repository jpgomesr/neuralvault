package workspaces

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestEnsureMember(t *testing.T) {
	cases := []struct {
		name       string
		member     bool
		memberErr  error
		wantOK     bool
		wantStatus int
	}{
		{name: "member proceeds", member: true, wantOK: true, wantStatus: http.StatusOK},
		{name: "non-member forbidden", member: false, wantOK: false, wantStatus: http.StatusForbidden},
		{name: "lookup error", memberErr: errors.New("boom"), wantOK: false, wantStatus: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{member: tc.member, memberErr: tc.memberErr}
			req := httptest.NewRequest(http.MethodGet, "/query", nil)
			rec := httptest.NewRecorder()

			ok := EnsureMember(rec, req, svc, uuid.New())

			if ok != tc.wantOK {
				t.Fatalf("EnsureMember returned %v, want %v", ok, tc.wantOK)
			}
			// A passing check writes nothing, leaving the recorder's default 200.
			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}
