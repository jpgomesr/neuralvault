package modelconfig

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jpgomesr/NeuralVault/internal/catalog"
	"github.com/jpgomesr/NeuralVault/internal/httperr"
	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/workspaces"
)

// Reindexer re-runs ingestion for every source in a workspace, returning how
// many were queued.
//
// It is declared here, as the narrow interface this domain consumes, rather than
// depending on the whole sources package — which would be a cycle waiting to
// happen, since sources already depends on the resolvers this package implements.
// Satisfied by sources.SourceService.
type Reindexer interface {
	ReindexWorkspace(ctx context.Context, workspaceID uuid.UUID) (int, error)
}

// Handler holds HTTP handler methods for the model-configuration domain.
type Handler struct {
	service   Service
	members   workspaces.Service
	reindexer Reindexer
}

// NewHandler returns a Handler backed by service.
func NewHandler(service Service, members workspaces.Service, reindexer Reindexer) *Handler {
	return &Handler{service: service, members: members, reindexer: reindexer}
}

// workspaceID parses the {workspace_id} path parameter and enforces membership.
// It reports whether the request may proceed; on failure the response has
// already been written.
func (h *Handler) workspaceID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "workspace_id"))
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return uuid.Nil, false
	}

	if !workspaces.EnsureMember(w, r, h.members, id) {
		return uuid.Nil, false
	}
	return id, true
}

// provider parses the {provider} path parameter.
func provider(r *http.Request) catalog.Provider {
	return catalog.Provider(chi.URLParam(r, "provider"))
}

// writeServiceError maps a service error to a status code.
//
// ErrInvalidProvider and ErrCredentialNotFound are the user's mistakes (an
// unknown provider, a model chosen with no key saved) and their messages are
// safe to return. ErrProviderUnavailable carries the upstream provider's own
// message — "Invalid API Key", "model not found" — which is exactly what the
// user needs to fix their configuration, and contains no NeuralVault internals.
// Anything else is a genuine 500 and goes through httperr, which never leaks
// detail.
func writeServiceError(w http.ResponseWriter, r *http.Request, logMsg string, err error, kv ...any) {
	switch {
	case errors.Is(err, ErrInvalidProvider), errors.Is(err, ErrCredentialNotFound):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrProviderUnavailable):
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		httperr.Internal(w, r, logMsg, err, kv...)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// ListProviders godoc
//
// @Summary List model providers
// @Description Returns the provider catalog annotated with which providers this workspace has an API key for. The key itself is never returned, only a 4-character hint.
// @Tags modelconfig
// @Produce json
// @Param workspace_id path string true "Workspace ID"
// @Success 200 {array} modelconfig.ProviderStatus
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /workspaces/{workspace_id}/providers [get]
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}

	providers, err := h.service.Providers(r.Context(), workspaceID)
	if err != nil {
		httperr.Internal(w, r, "list providers failed", err, "workspace_id", workspaceID)
		return
	}

	writeJSON(w, http.StatusOK, providers)
}

// saveCredentialRequest is the JSON body accepted by PUT
// /workspaces/{workspace_id}/providers/{provider}/credential.
type saveCredentialRequest struct {
	APIKey string `json:"api_key"`
	// BaseURL overrides the provider's default endpoint. Optional.
	BaseURL string `json:"base_url,omitempty"`
}

// SaveCredential godoc
//
// @Summary Save a provider API key
// @Description Stores an API key for a provider (BYOK), encrypted at rest. The key is validated against the provider before being saved, so an invalid key is rejected here rather than failing later mid-answer.
// @Tags modelconfig
// @Accept json
// @Produce json
// @Param workspace_id path string true "Workspace ID"
// @Param provider path string true "Provider ID"
// @Success 204
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 502
// @Failure 500
// @Router /workspaces/{workspace_id}/providers/{provider}/credential [put]
func (h *Handler) SaveCredential(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}

	var req saveCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.APIKey == "" {
		http.Error(w, "api_key is required", http.StatusBadRequest)
		return
	}

	p := provider(r)
	if err := h.service.SaveCredential(r.Context(), workspaceID, p, req.APIKey, req.BaseURL); err != nil {
		// The error is logged without the request body: it holds the API key.
		writeServiceError(w, r, "save provider credential failed", err, "workspace_id", workspaceID, "provider", p)
		return
	}

	slog.InfoContext(r.Context(), "provider credential saved",
		"workspace_id", workspaceID, "provider", p, "request_id", logger.RequestID(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

// DeleteCredential godoc
//
// @Summary Delete a provider API key
// @Description Removes the workspace's stored API key for a provider.
// @Tags modelconfig
// @Param workspace_id path string true "Workspace ID"
// @Param provider path string true "Provider ID"
// @Success 204
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /workspaces/{workspace_id}/providers/{provider}/credential [delete]
func (h *Handler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}

	p := provider(r)
	if err := h.service.DeleteCredential(r.Context(), workspaceID, p); err != nil {
		httperr.Internal(w, r, "delete provider credential failed", err, "workspace_id", workspaceID, "provider", p)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListModels godoc
//
// @Summary List a provider's models
// @Description Lists the models the workspace's API key can reach, fetched live from the provider rather than from a hardcoded list.
// @Tags modelconfig
// @Produce json
// @Param workspace_id path string true "Workspace ID"
// @Param provider path string true "Provider ID"
// @Success 200 {array} types.ModelInfo
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 502
// @Failure 500
// @Router /workspaces/{workspace_id}/providers/{provider}/models [get]
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}

	p := provider(r)
	models, err := h.service.Models(r.Context(), workspaceID, p)
	if err != nil {
		writeServiceError(w, r, "list provider models failed", err, "workspace_id", workspaceID, "provider", p)
		return
	}

	writeJSON(w, http.StatusOK, models)
}

// settingsResponse is the JSON shape of a workspace's model settings. Empty
// fields mean the workspace is on the server's default provider.
type settingsResponse struct {
	LLMProvider catalog.Provider `json:"llm_provider,omitempty"`
	LLMModel    string           `json:"llm_model,omitempty"`

	EmbeddingProvider   catalog.Provider `json:"embedding_provider,omitempty"`
	EmbeddingModel      string           `json:"embedding_model,omitempty"`
	EmbeddingDimensions uint64           `json:"embedding_dimensions,omitempty"`
}

// GetSettings godoc
//
// @Summary Get a workspace's model settings
// @Description Returns the workspace's chosen completion and embedding models. Empty fields mean it uses the server default (Ollama).
// @Tags modelconfig
// @Produce json
// @Param workspace_id path string true "Workspace ID"
// @Success 200 {object} modelconfig.settingsResponse
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /workspaces/{workspace_id}/model-settings [get]
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}

	settings, err := h.service.Settings(r.Context(), workspaceID)
	if err != nil {
		httperr.Internal(w, r, "get model settings failed", err, "workspace_id", workspaceID)
		return
	}

	writeJSON(w, http.StatusOK, settingsResponse{
		LLMProvider:         settings.LLMProvider,
		LLMModel:            settings.LLMModel,
		EmbeddingProvider:   settings.EmbeddingProvider,
		EmbeddingModel:      settings.EmbeddingModel,
		EmbeddingDimensions: settings.EmbeddingDimensions,
	})
}

// setLLMRequest is the JSON body accepted by PUT /workspaces/{id}/model-settings/llm.
type setLLMRequest struct {
	Provider catalog.Provider `json:"provider"`
	Model    string           `json:"model"`
}

// SetLLM godoc
//
// @Summary Set the workspace's completion model
// @Description Sets the default provider and model used to answer queries. Changing it affects only the next query — nothing is re-indexed.
// @Tags modelconfig
// @Accept json
// @Param workspace_id path string true "Workspace ID"
// @Success 204
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /workspaces/{workspace_id}/model-settings/llm [put]
func (h *Handler) SetLLM(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}

	var req setLLMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.service.SetLLM(r.Context(), workspaceID, req.Provider, req.Model); err != nil {
		writeServiceError(w, r, "set llm settings failed", err, "workspace_id", workspaceID, "provider", req.Provider)
		return
	}

	slog.InfoContext(r.Context(), "llm settings updated",
		"workspace_id", workspaceID, "provider", req.Provider, "model", req.Model,
		"request_id", logger.RequestID(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

// setEmbeddingRequest is the JSON body accepted by PUT
// /workspaces/{id}/model-settings/embedding.
type setEmbeddingRequest struct {
	Provider catalog.Provider `json:"provider"`
	Model    string           `json:"model"`
}

// SetEmbedding godoc
//
// @Summary Set the workspace's embedding model
// @Description Sets the embedding provider and model. The provider is probed for its vector dimension and a matching Qdrant collection is created. Existing sources were embedded with the previous model and are NOT migrated: the response reports how many need re-indexing, and until they are, retrieval finds nothing.
// @Tags modelconfig
// @Accept json
// @Produce json
// @Param workspace_id path string true "Workspace ID"
// @Success 200 {object} modelconfig.EmbeddingChange
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 502
// @Failure 500
// @Router /workspaces/{workspace_id}/model-settings/embedding [put]
func (h *Handler) SetEmbedding(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}

	var req setEmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	change, err := h.service.SetEmbedding(r.Context(), workspaceID, req.Provider, req.Model)
	if err != nil {
		writeServiceError(w, r, "set embedding settings failed", err, "workspace_id", workspaceID, "provider", req.Provider)
		return
	}

	slog.InfoContext(r.Context(), "embedding settings updated",
		"workspace_id", workspaceID, "provider", req.Provider, "model", req.Model,
		"collection", change.Collection, "dimensions", change.Dimensions,
		"stale_sources", change.StaleSources,
		"request_id", logger.RequestID(r.Context()))

	writeJSON(w, http.StatusOK, change)
}

// reindexResponse reports how many sources were queued for re-indexing.
type reindexResponse struct {
	Queued int `json:"queued"`
}

// Reindex godoc
//
// @Summary Re-index every source in the workspace
// @Description Queues re-ingestion of all the workspace's sources, re-embedding them with the current embedding model. Required after changing the embedding model, since a source's existing vectors belong to the previous model's collection. Returns immediately; progress streams per source over /sources/{id}/status.
// @Tags modelconfig
// @Produce json
// @Param workspace_id path string true "Workspace ID"
// @Success 202 {object} modelconfig.reindexResponse
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /workspaces/{workspace_id}/reindex [post]
func (h *Handler) Reindex(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}

	queued, err := h.reindexer.ReindexWorkspace(r.Context(), workspaceID)
	if err != nil {
		httperr.Internal(w, r, "workspace reindex failed", err, "workspace_id", workspaceID)
		return
	}

	slog.InfoContext(r.Context(), "workspace reindex queued",
		"workspace_id", workspaceID, "queued", queued, "request_id", logger.RequestID(r.Context()))

	writeJSON(w, http.StatusAccepted, reindexResponse{Queued: queued})
}
