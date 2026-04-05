package api

import (
	"log/slog"
	"net/http"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

func (h *Handlers) HandleRemoveTask(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[swarm.Task]{
		resource:     "task",
		pathKey:      "id",
		getter:       h.cache.GetTask,
		remove:       h.resourceRemover.RemoveTask,
		conflictCode: "TSK001",
	})
}

func (h *Handlers) HandleRemoveNetwork(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[network.Summary]{
		resource:     "network",
		pathKey:      "id",
		getter:       h.cache.GetNetwork,
		remove:       h.resourceRemover.RemoveNetwork,
		conflictCode: "NET001",
	})
}

func (h *Handlers) HandleRemoveVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if _, ok := lookupOr404(w, r, "volume", name, h.cache.GetVolume); !ok {
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
