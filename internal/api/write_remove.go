package api

import (
	"context"
	"log/slog"
	"net/http"

	cerrdefs "github.com/containerd/errdefs"
)

// removeSpec describes how to remove a resource. Used by handleRemove to
// eliminate per-resource boilerplate for simple (non-force) removal handlers.
type removeSpec[T any] struct {
	resource     string // e.g. "task", "config"
	pathKey      string // path value key: "id" or "name"
	getter       func(string) (T, bool)
	remove       func(ctx context.Context, key string) error
	conflictCode string
}

func handleRemove[T any](
	w http.ResponseWriter,
	r *http.Request,
	spec removeSpec[T],
) {
	key := r.PathValue(spec.pathKey)

	if _, ok := lookupOr404(w, r, spec.resource, key, spec.getter); !ok {
		return
	}

	slog.Info("removing "+spec.resource, spec.resource, key)

	if err := spec.remove(r.Context(), key); err != nil {
		if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
			writeErrorCode(w, r, spec.conflictCode, err.Error())
			return
		}

		writeDockerError(w, r, err, spec.resource, key)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
