package api

import (
	"log/slog"
	"net/http"

	"github.com/docker/docker/errdefs"
	json "github.com/goccy/go-json"
)

type updateImageRequest struct {
	Image string `json:"image"`
}

type scaleRequest struct {
	Replicas *uint64 `json:"replicas"`
}

func (h *Handlers) HandleScaleService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req scaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Replicas == nil {
		writeProblem(w, r, http.StatusBadRequest, "replicas is required")
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	if svc.Spec.Mode.Replicated == nil {
		writeProblem(w, r, http.StatusBadRequest, "cannot scale a global-mode service")
		return
	}

	slog.Info("scaling service", "service", id, "replicas", *req.Replicas)

	updated, err := h.writeClient.ScaleService(r.Context(), id, *req.Replicas)
	if err != nil {
		if errdefs.IsNotFound(err) {
			writeProblem(w, r, http.StatusNotFound, "service not found")
			return
		}
		if errdefs.IsConflict(err) {
			writeProblem(w, r, http.StatusConflict, "service was modified by another client, please retry")
			return
		}
		slog.Error("failed to scale service", "service", id, "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to scale service")
		return
	}

	// Use writeJSON (not writeJSONWithETag) for mutation responses:
	// ETag + If-None-Match → 304 is only valid for safe methods (GET/HEAD)
	// per RFC 9110 Section 13.1.1.
	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}

func (h *Handlers) HandleUpdateServiceImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req updateImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Image == "" {
		writeProblem(w, r, http.StatusBadRequest, "image is required")
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	slog.Info("updating service image", "service", id, "image", req.Image)

	updated, err := h.writeClient.UpdateServiceImage(r.Context(), id, req.Image)
	if err != nil {
		if errdefs.IsNotFound(err) {
			writeProblem(w, r, http.StatusNotFound, "service not found")
			return
		}
		if errdefs.IsConflict(err) {
			writeProblem(w, r, http.StatusConflict, "service was modified by another client, please retry")
			return
		}
		slog.Error("failed to update service image", "service", id, "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to update service image")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}

func (h *Handlers) HandleRollbackService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	if svc.PreviousSpec == nil {
		writeProblem(w, r, http.StatusBadRequest, "service has no previous spec to rollback to")
		return
	}

	slog.Info("rolling back service", "service", id)

	updated, err := h.writeClient.RollbackService(r.Context(), id)
	if err != nil {
		if errdefs.IsNotFound(err) {
			writeProblem(w, r, http.StatusNotFound, "service not found")
			return
		}
		if errdefs.IsConflict(err) {
			writeProblem(w, r, http.StatusConflict, "service was modified by another client, please retry")
			return
		}
		slog.Error("failed to rollback service", "service", id, "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to rollback service")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}

func (h *Handlers) HandleRestartService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	slog.Info("restarting service", "service", id)

	updated, err := h.writeClient.RestartService(r.Context(), id)
	if err != nil {
		if errdefs.IsNotFound(err) {
			writeProblem(w, r, http.StatusNotFound, "service not found")
			return
		}
		if errdefs.IsConflict(err) {
			writeProblem(w, r, http.StatusConflict, "service was modified by another client, please retry")
			return
		}
		slog.Error("failed to restart service", "service", id, "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to restart service")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}
