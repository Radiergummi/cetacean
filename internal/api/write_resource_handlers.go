package api

import (
	"context"
	"net/http"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

func (h *Handlers) HandleRemoveTask(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[swarm.Task]{
		resource: "task",
		getter:   h.cache.GetTask,
		remove: func(ctx context.Context, key string) error {
			return h.writes.RemoveTask(ctx, h.caller(r), key)
		},
		conflictCode: "TSK001",
	})
}

func (h *Handlers) HandleRemoveNetwork(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[network.Summary]{
		resource: "network",
		getter:   h.cache.GetNetwork,
		remove: func(ctx context.Context, key string) error {
			return h.writes.RemoveNetwork(ctx, h.caller(r), key)
		},
		conflictCode: "NET001",
	})
}

func (h *Handlers) HandleRemoveVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if _, ok := lookupOr404(w, r, "volume", name, h.cache.GetVolume); !ok {
		return
	}

	force := r.URL.Query().Get("force") == "true"

	err := h.writes.RemoveVolume(r.Context(), h.caller(r), name, force)
	if err != nil {
		writeRemovalError(w, r, err, "VOL001", "volume", name)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
