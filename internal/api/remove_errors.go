package api

import (
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
)

// The engine reports a removal's precondition with whatever status its
// subsystem chose: an in-use network is a 403, an undrained node a 400. These
// are the messages that mark them, matched where the class alone cannot.
var removalConflictMessages = map[string][]string{
	"network": {"has active endpoints"},
	"node":    {"is not down", "must be demoted"},
	"volume":  {"volume is in use"},
}

// isRemovalConflict reports whether err refused a removal because of the
// resource's state, which the caller can fix, rather than a failure.
func isRemovalConflict(err error, resource string) bool {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		return true
	}

	message := err.Error()
	for _, marker := range removalConflictMessages[resource] {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

// writeRemovalConflict answers a refused removal with its code and a detail of
// our own: the engine's text names the containers or services in the way,
// which the caller may not be allowed to see. The log keeps it.
func writeRemovalConflict(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	code, resource, key string,
) {
	slog.Info("removal refused", "resource", resource, "id", key, "error", err)
	writeErrorCode(w, r, code, resource+" "+key+" cannot be removed in its current state")
}
