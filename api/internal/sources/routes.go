package sources

import (
	"github.com/go-chi/chi/v5"
)

// Routes returns a Chi router with all source endpoints mounted.
func Routes(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.CreateSource)
	r.Get("/", h.ListSources)
	r.Post("/{id}/ingest", h.IngestSource)
	r.Get("/{id}/chunks", h.ListChunks)
	r.Get("/{id}/files", h.ListFiles)
	r.Get("/{id}/files/content", h.GetFileContent)
	r.Get("/{id}/status", h.StreamStatus)
	return r
}
