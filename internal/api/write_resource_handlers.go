package api

import (
	"log/slog"
	"net/http"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
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
		// On a local volume force overrides driver errors, not use; Docker
		// refuses an in-use local volume either way.
		if cluster.IsRemovalConflict(err, "volume") {
			writeRemovalConflict(w, r, err, "VOL001", "volume", name)
			return
		}
		writeDockerError(w, r, err, "volume", name)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
