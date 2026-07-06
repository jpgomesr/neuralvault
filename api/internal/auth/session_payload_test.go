package auth

import (
	"encoding/base64"
	"testing"
)

// These cases reach the payload-decoding branches of Verify: the signature is
// valid (computed over `encoded`), so verification proceeds past the HMAC check
// and fails while decoding or unmarshaling the payload.

func TestSessionSigner_Verify_UndecodablePayload(t *testing.T) {
	s := newSessionSigner(testSecret)
	encoded := "!!!not-base64!!!" // not valid base64url, but carries a valid signature
	token := encoded + "." + s.sign(encoded)

	if _, err := s.Verify(token); err != ErrInvalidToken {
		t.Fatalf("Verify: got %v, want %v", err, ErrInvalidToken)
	}
}

func TestSessionSigner_Verify_NonJSONPayload(t *testing.T) {
	s := newSessionSigner(testSecret)
	encoded := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	token := encoded + "." + s.sign(encoded)

	if _, err := s.Verify(token); err != ErrInvalidToken {
		t.Fatalf("Verify: got %v, want %v", err, ErrInvalidToken)
	}
}
