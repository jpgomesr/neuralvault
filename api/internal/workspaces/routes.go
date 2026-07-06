package workspaces

import (
	"github.com/go-chi/chi/v5"
)

// Routes returns a Chi router with the workspace endpoints mounted. Both are
// protected by RequireUser at the mount point in router.go.
func Routes(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	return r
}
