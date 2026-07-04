package auth

import (
	"log/slog"
	"net/http"

	"github.com/jpgomesr/NeuralVault/internal/logger"
)

// RequireUser is middleware that authenticates the caller from the session
// cookie. On success it stores the resolved Principal in the request context
// (read via UserID / principalFrom); otherwise it responds 401 and stops the
// chain. Mirrors the middleware.RequestID → context-accessor pattern.
func (h *Handler) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			h.unauthorized(w, r, "missing session")
			return
		}

		claims, err := h.signer.Verify(cookie.Value)
		if err != nil {
			h.unauthorized(w, r, "invalid session")
			return
		}

		ctx := withPrincipal(r.Context(), Principal{UserID: claims.UserID, Email: claims.Email})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// unauthorized logs the reason and writes a 401 response.
func (h *Handler) unauthorized(w http.ResponseWriter, r *http.Request, reason string) {
	slog.WarnContext(r.Context(), "unauthorized request", "reason", reason, "path", r.URL.Path, "request_id", logger.RequestID(r.Context()))
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
