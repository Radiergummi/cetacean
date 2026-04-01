package api

import (
	"fmt"
	"log/slog"
	"net/http"

	cerrdefs "github.com/containerd/errdefs"
)

func (h *Handlers) HandleRemoveTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetTask(id)
	if !ok {
		writeErrorCode(w, r, "TSK002", fmt.Sprintf("task %q not found", id))
		return
	}

	slog.Info("removing task", "task", id)

	err := h.resourceRemover.RemoveTask(r.Context(), id)
	if err != nil {
		if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
			writeErrorCode(w, r, "TSK001", err.Error())
			return
		}
		writeDockerError(w, r, err, "task", id)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRemoveNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetNetwork(id)
	if !ok {
		writeErrorCode(w, r, "NET002", fmt.Sprintf("network %q not found", id))
		return
	}

	slog.Info("removing network", "network", id)

	err := h.resourceRemover.RemoveNetwork(r.Context(), id)
	if err != nil {
		if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
			writeErrorCode(w, r, "NET001", err.Error())
			return
		}
		writeDockerError(w, r, err, "network", id)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRemoveVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	_, ok := h.cache.GetVolume(name)
	if !ok {
		writeErrorCode(w, r, "VOL002", fmt.Sprintf("volume %q not found", name))
		return
	}

	force := r.URL.Query().Get("force") == "true"

	slog.Info("removing volume", "volume", name, "force", force)

	err := h.resourceRemover.RemoveVolume(r.Context(), name, force)
	if err != nil {
		if !force && (cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err)) {
			writeErrorCode(w, r, "VOL001", err.Error())
			return
		}
		writeDockerError(w, r, err, "volume", name)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
