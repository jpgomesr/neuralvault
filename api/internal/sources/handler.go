package sources

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jpgomesr/NeuralVault/internal/logger"
	"github.com/jpgomesr/NeuralVault/internal/model"
	"github.com/jpgomesr/NeuralVault/internal/workspaces"
)

// Handler holds HTTP handler methods for the sources domain.
type Handler struct {
	service Service
	bus     *ProgressBus
	members workspaces.Service
}

// NewHandler returns a Handler backed by service and bus. members enforces
// that the caller belongs to the workspace named in each request.
func NewHandler(service Service, bus *ProgressBus, members workspaces.Service) *Handler {
	return &Handler{service: service, bus: bus, members: members}
}

// CreateSource godoc
//
// Accepts multipart/form-data with fields workspace_id, name, and one or more files.
// Uploads files to object storage and returns 202 immediately; indexing runs in background.
//
// @Summary Create a source
// @Description Uploads one or more files to object storage and starts background indexing. Returns the created source and its status_url.
// @Tags sources
// @Accept mpfd
// @Produce json
// @Param workspace_id formData string true "Workspace UUID"
// @Param name formData string true "Source name"
// @Param files formData file true "Files to ingest (repeatable)"
// @Success 202
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /sources [post]
func (h *Handler) CreateSource(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		slog.WarnContext(r.Context(), "invalid create source request", "err", err, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}

	workspaceID, err := uuid.Parse(r.FormValue("workspace_id"))
	if err != nil {
		slog.WarnContext(r.Context(), "invalid create source request", "err", "invalid workspace_id", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid workspace_id: must be a UUID", http.StatusBadRequest)
		return
	}

	if !workspaces.EnsureMember(w, r, h.members, workspaceID) {
		return
	}

	name := r.FormValue("name")
	if name == "" {
		slog.WarnContext(r.Context(), "invalid create source request", "err", "name is required", "workspace_id", workspaceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	fhs := r.MultipartForm.File["files"]
	if len(fhs) == 0 {
		slog.WarnContext(r.Context(), "invalid create source request", "err", "at least one file is required", "workspace_id", workspaceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "at least one file is required", http.StatusBadRequest)
		return
	}

	var uploads []FileUpload
	for _, fh := range fhs {
		f, err := fh.Open()
		if err != nil {
			slog.WarnContext(r.Context(), "invalid create source request", "err", err, "workspace_id", workspaceID, "request_id", logger.RequestID(r.Context()))
			http.Error(w, "failed to open uploaded file: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer f.Close() //nolint:errcheck
		uploads = append(uploads, FileUpload{
			Name:    filepath.Base(fh.Filename),
			Content: f,
			Size:    fh.Size,
		})
	}

	source, err := h.service.Create(r.Context(), CreateRequest{
		WorkspaceID: workspaceID,
		Name:        name,
	}, uploads)
	if err != nil {
		slog.ErrorContext(r.Context(), "create source failed", "err", err, "workspace_id", workspaceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to create source: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"source":     source,
		"status_url": fmt.Sprintf("/sources/%s/status", source.ID),
	})
}

// ListSources godoc
//
// @Summary List sources
// @Description Returns the sources of a workspace the caller belongs to.
// @Tags sources
// @Produce json
// @Param workspace_id query string true "Workspace UUID"
// @Success 200
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /sources [get]
func (h *Handler) ListSources(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(r.URL.Query().Get("workspace_id"))
	if err != nil {
		slog.WarnContext(r.Context(), "invalid list sources request", "err", "invalid workspace_id", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid workspace_id: must be a UUID", http.StatusBadRequest)
		return
	}

	if !workspaces.EnsureMember(w, r, h.members, workspaceID) {
		return
	}

	sources, err := h.service.List(r.Context(), workspaceID)
	if err != nil {
		slog.ErrorContext(r.Context(), "list sources failed", "err", err, "workspace_id", workspaceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to list sources: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sources) //nolint:errcheck
}

// IngestSource godoc
//
// Re-downloads files from object storage and re-indexes in the background.
// Returns 202 immediately; progress via GET /sources/{id}/status.
//
// @Summary Re-ingest a source
// @Description Re-downloads the source's files from object storage and re-indexes them in the background.
// @Tags sources
// @Produce json
// @Param id path string true "Source UUID"
// @Success 202
// @Failure 400
// @Failure 401
// @Failure 500
// @Router /sources/{id}/ingest [post]
func (h *Handler) IngestSource(w http.ResponseWriter, r *http.Request) {
	sourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		slog.WarnContext(r.Context(), "invalid ingest source request", "err", "invalid source id", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid source id: must be a UUID", http.StatusBadRequest)
		return
	}

	if err := h.service.Ingest(r.Context(), sourceID); err != nil {
		slog.ErrorContext(r.Context(), "ingest source failed", "err", err, "source_id", sourceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to start ingest: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"source_id":  sourceID,
		"status_url": fmt.Sprintf("/sources/%s/status", sourceID),
	})
}

// ListChunks godoc
//
// @Summary List chunks of a source
// @Description Returns the indexed chunks produced from a source.
// @Tags sources
// @Produce json
// @Param id path string true "Source UUID"
// @Success 200
// @Failure 400
// @Failure 401
// @Failure 500
// @Router /sources/{id}/chunks [get]
func (h *Handler) ListChunks(w http.ResponseWriter, r *http.Request) {
	sourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		slog.WarnContext(r.Context(), "invalid list chunks request", "err", "invalid source id", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid source id: must be a UUID", http.StatusBadRequest)
		return
	}

	chunks, err := h.service.ListChunks(r.Context(), sourceID)
	if err != nil {
		slog.ErrorContext(r.Context(), "list chunks failed", "err", err, "source_id", sourceID, "request_id", logger.RequestID(r.Context()))
		http.Error(w, "failed to list chunks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chunks) //nolint:errcheck
}

// StreamStatus godoc
//
// Sends progress events while indexing is in progress, then a terminal
// EventDone or EventError event and closes the connection.
//
// The client should subscribe to this endpoint immediately after POST /sources
// to avoid missing events. If the source is already in a terminal state when
// the client connects, the terminal event is sent right away.
//
// @Summary Stream indexing status (SSE)
// @Description Server-Sent Events stream of indexing progress: progress events, then a terminal done or error event. Heartbeat every 30s; 15min timeout.
// @Tags sources
// @Param id path string true "Source UUID"
// @Success 200
// @Failure 400
// @Failure 401
// @Failure 500
// @Router /sources/{id}/status [get]
func (h *Handler) StreamStatus(w http.ResponseWriter, r *http.Request) {
	sourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		slog.WarnContext(r.Context(), "invalid stream status request", "err", "invalid source id", "request_id", logger.RequestID(r.Context()))
		http.Error(w, "invalid source id: must be a UUID", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported by this server", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Subscribe before checking DB state to avoid a race where the goroutine
	// finishes and publishes EventDone between our DB read and our select.
	ch, cancel := h.bus.Subscribe(sourceID)
	defer cancel()

	// If already in a terminal state, send the final event and return immediately.
	source, err := h.service.GetByID(r.Context(), sourceID)
	if err != nil {
		writeSSE(w, ProgressEvent{Type: EventError, Error: "source not found"})
		flusher.Flush()
		return
	}

	switch source.Status {
	case model.SourceStatusIndexed:
		writeSSE(w, ProgressEvent{Type: EventDone})
		flusher.Flush()
		return
	case model.SourceStatusError:
		writeSSE(w, ProgressEvent{Type: EventError, Error: "indexing failed"})
		flusher.Flush()
		return
	}

	// Heartbeat keeps the TCP connection alive through proxies and load balancers.
	// Timeout guards against a source stuck in indexing (e.g. after a server restart).
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	timeout := time.NewTimer(15 * time.Minute)
	defer timeout.Stop()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, event)
			flusher.Flush()
			if event.Type == EventDone || event.Type == EventError {
				return
			}
		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n") //nolint:errcheck
			flusher.Flush()
		case <-timeout.C:
			writeSSE(w, ProgressEvent{Type: EventError, Error: "indexing timed out"})
			flusher.Flush()
			return
		case <-r.Context().Done():
			return
		}
	}
}

// writeSSE formats a ProgressEvent as an SSE data line.
func writeSSE(w http.ResponseWriter, event ProgressEvent) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "data: %s\n\n", data) //nolint:errcheck
}
