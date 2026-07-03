package retrieval

import (
	"github.com/go-chi/chi/v5"
)

// Routes returns a Chi router with the query endpoint mounted.
func Routes(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Query)
	return r
}
