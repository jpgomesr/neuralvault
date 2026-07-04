package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

const testSecret = "test-session-secret-at-least-32-bytes"

func TestSessionSigner_IssueVerify_RoundTrip(t *testing.T) {
	s := newSessionSigner(testSecret)
	userID := uuid.New()

	token, err := s.Issue(userID, "user@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.UserID != userID {
		t.Fatalf("UserID: got %s, want %s", claims.UserID, userID)
	}
	if claims.Email != "user@example.com" {
		t.Fatalf("Email: got %q, want %q", claims.Email, "user@example.com")
	}
}

func TestSessionSigner_Verify_Rejects(t *testing.T) {
	s := newSessionSigner(testSecret)
	valid, err := s.Issue(uuid.New(), "user@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	cases := []struct {
		name  string
		token string
		want  error
	}{
		{name: "empty", token: "", want: ErrInvalidToken},
		{name: "no separator", token: "notoken", want: ErrInvalidToken},
		{name: "tampered payload", token: "tampered." + valid[len(valid)-43:], want: ErrInvalidToken},
		{name: "tampered signature", token: valid[:len(valid)-4] + "AAAA", want: ErrInvalidToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Verify(tc.token); err != tc.want {
				t.Fatalf("Verify(%q): got %v, want %v", tc.name, err, tc.want)
			}
		})
	}
}

func TestSessionSigner_Verify_WrongSecret(t *testing.T) {
	issuer := newSessionSigner(testSecret)
	token, err := issuer.Issue(uuid.New(), "user@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	other := newSessionSigner("a-completely-different-secret-32bytes!")
	if _, err := other.Verify(token); err != ErrInvalidToken {
		t.Fatalf("Verify with wrong secret: got %v, want %v", err, ErrInvalidToken)
	}
}

func TestSessionSigner_Verify_Expired(t *testing.T) {
	base := time.Now()
	issuer := newSessionSigner(testSecret)
	issuer.now = func() time.Time { return base }

	token, err := issuer.Issue(uuid.New(), "user@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Verify from a point in time past the TTL.
	issuer.now = func() time.Time { return base.Add(sessionTTL + time.Second) }
	if _, err := issuer.Verify(token); err != ErrExpiredToken {
		t.Fatalf("Verify expired: got %v, want %v", err, ErrExpiredToken)
	}
}
