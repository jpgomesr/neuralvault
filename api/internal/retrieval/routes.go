package retrieval

import (
	"github.com/go-chi/chi/v5"
)

// Routes returns a Chi router with the query endpoints mounted: the
// non-streaming JSON endpoint (for the CLI) and the SSE streaming endpoint.
func Routes(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Query)
	r.Post("/stream", h.QueryStream)
	return r
}
