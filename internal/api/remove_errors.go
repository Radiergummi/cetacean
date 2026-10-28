package api

import (
	"log/slog"
	"net/http"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// writeRemovalError answers a refused removal with code and cluster's detail
// when the resource's state refused it, and otherwise as writeDockerError.
// The log keeps the engine's text.
func writeRemovalError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	code, resource, key string,
) {
	refusal, ok := cluster.ClassifyRemoval(err, resource, key)
	if !ok {
		refusal = &cluster.WriteError{Kind: cluster.Engine, Resource: resource, Err: err}
	}

	if refusal.Kind != cluster.Conflict {
		writeRefusal(w, r, refusal, "")
		return
	}

	slog.InfoContext(r.Context(), "removal refused", "resource", resource, "id", key, "error", err)
	problem.Write(w, r, code, refusal.Detail)
}
