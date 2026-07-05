package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestUserID(t *testing.T) {
	if got := UserID(context.Background()); got != uuid.Nil {
		t.Fatalf("UserID on a context with no principal: got %s, want %s", got, uuid.Nil)
	}

	id := uuid.New()
	ctx := withPrincipal(context.Background(), Principal{UserID: id, Email: "x@example.com"})
	if got := UserID(ctx); got != id {
		t.Fatalf("UserID: got %s, want %s", got, id)
	}
}

func TestPrincipalFrom(t *testing.T) {
	if _, ok := principalFrom(context.Background()); ok {
		t.Fatal("expected no principal on a bare context")
	}

	p := Principal{UserID: uuid.New(), Email: "x@example.com"}
	got, ok := principalFrom(withPrincipal(context.Background(), p))
	if !ok {
		t.Fatal("expected a principal to be present")
	}
	if got != p {
		t.Fatalf("principal: got %+v, want %+v", got, p)
	}
}
