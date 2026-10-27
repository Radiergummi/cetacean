package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// removeSpec describes how to remove a resource addressed by the "id" path
// value. Used by handleRemove for the simple (non-force) removal handlers.
type removeSpec[T any] struct {
	resource     string // e.g. "task", "config"
	getter       func(string) (T, bool)
	remove       func(ctx context.Context, key string) error
	conflictCode string
}

func handleRemove[T any](
	w http.ResponseWriter,
	r *http.Request,
	spec removeSpec[T],
) {
	key := r.PathValue("id")

	if _, ok := lookupOr404(w, r, spec.resource, key, spec.getter); !ok {
		return
	}

	slog.Info("removing "+spec.resource, spec.resource, key)

	if err := spec.remove(r.Context(), key); err != nil {
		if cluster.IsRemovalConflict(err, spec.resource) {
			writeRemovalConflict(w, r, err, spec.conflictCode, spec.resource, key)
			return
		}

		writeDockerError(w, r, err, spec.resource, key)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
