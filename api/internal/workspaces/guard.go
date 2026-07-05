package workspaces

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/auth"
	"github.com/jpgomesr/NeuralVault/internal/logger"
)

// EnsureMember enforces tenant isolation for a workspace-scoped request. It
// verifies the authenticated caller belongs to workspaceID and returns true if
// the handler may proceed. Otherwise it writes the appropriate response —
// 403 when the caller is not a member, 500 on a lookup error — and returns
// false, so callers should `return` immediately.
//
// Centralising the check here keeps the isolation rule in one place: a valid
// session can never touch a workspace it does not belong to, regardless of
// which endpoint (or which workspace_id encoding) the request used.
func EnsureMember(w http.ResponseWriter, r *http.Request, svc Service, workspaceID uuid.UUID) bool {
	userID := auth.UserID(r.Context())

	member, err := svc.IsMember(r.Context(), userID, workspaceID)
	if err != nil {
		slog.ErrorContext(r.Context(), "membership check failed", "err", err, "user_id", userID, "workspace_id", workspaceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to verify workspace access", http.StatusInternalServerError)
		return false
	}
	if !member {
		slog.WarnContext(r.Context(), "workspace access denied", "user_id", userID, "workspace_id", workspaceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "forbidden: not a member of this workspace", http.StatusForbidden)
		return false
	}
	return true
}
