package api

import (
	"log/slog"
	"net/http"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// writeRemovalConflict answers a refused removal with its code and a detail of
// our own, as the caller may not be allowed to see what is in the way. The log
// keeps the engine's text.
func writeRemovalConflict(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	code, resource, key string,
) {
	slog.Info("removal refused", "resource", resource, "id", key, "error", err)
	writeErrorCode(w, r, code, cluster.RemovalConflictDetail(resource, key))
}
