// Package auth implements the OIDC authorization-code login flow, just-in-time
// user provisioning, a stateless signed session cookie, and middleware that
// establishes the caller's identity on every protected request.
package auth

import (
	"context"

	"github.com/google/uuid"
)

// contextKey is an unexported type for context keys defined in this package,
// preventing collisions with keys from other packages.
type contextKey struct{}

// principalKey stores the authenticated Principal in the request context.
var principalKey = contextKey{}

// Principal is the authenticated caller resolved from the session cookie by
// RequireUser and stored in the request context.
type Principal struct {
	UserID uuid.UUID
	Email  string
}

// withPrincipal returns a copy of ctx carrying the authenticated principal.
func withPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// UserID returns the authenticated user's ID, or uuid.Nil if ctx carries no
// authenticated principal (e.g. a public route or background goroutine).
// Mirrors the logger.RequestID accessor pattern.
func UserID(ctx context.Context) uuid.UUID {
	if p, ok := ctx.Value(principalKey).(Principal); ok {
		return p.UserID
	}
	return uuid.Nil
}

// principalFrom returns the authenticated principal and whether one was present.
func principalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}
