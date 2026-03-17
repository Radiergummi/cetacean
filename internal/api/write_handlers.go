package api

import (
	"log/slog"
	"net/http"

	"github.com/docker/docker/errdefs"
	json "github.com/goccy/go-json"
)

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
