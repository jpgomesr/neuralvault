package modelconfig

import (
	"github.com/go-chi/chi/v5"
)

// Routes returns a Chi router with all model-configuration endpoints mounted.
//
// It is mounted under /workspaces/{workspace_id}, so every route here is
// workspace-scoped and each handler enforces membership before doing anything.
func Routes(h *Handler) chi.Router {
	r := chi.NewRouter()

	r.Get("/providers", h.ListProviders)
	r.Put("/providers/{provider}/credential", h.SaveCredential)
	r.Delete("/providers/{provider}/credential", h.DeleteCredential)
	r.Get("/providers/{provider}/models", h.ListModels)

	r.Get("/model-settings", h.GetSettings)
	r.Put("/model-settings/llm", h.SetLLM)
	r.Put("/model-settings/embedding", h.SetEmbedding)

	r.Post("/reindex", h.Reindex)

	return r
}
