package conversations

import (
	"github.com/go-chi/chi/v5"
)

// Routes returns a Chi router with all conversation endpoints mounted.
func Routes(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{id}/messages", h.ListMessages)
	return r
}
