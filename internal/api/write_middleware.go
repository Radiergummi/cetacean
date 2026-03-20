package api

import (
	"net/http"
	"strconv"
)

// requireLevel returns middleware that blocks requests when the configured
// operations level is below the required level for this endpoint.
//
// Levels:
//   - 0: read-only (all writes blocked)
//   - 1: operational (scale, restart, rollback, image update, env/labels/resources/healthcheck patches)
//   - 2: impactful (node availability/labels, service mode/endpoint-mode, task removal)
func requireLevel(required, configured int) func(http.HandlerFunc) http.Handler {
	return func(next http.HandlerFunc) http.Handler {
		if configured >= required {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeProblem(w, r, http.StatusForbidden,
				"this operation requires operations level "+strconv.Itoa(required)+
					", but the server is configured at level "+strconv.Itoa(configured))
		})
	}
}

// requireWrite is a temporary shim that will be removed in Task 4
// when the router is updated to use requireLevel directly.
func requireWrite(next http.HandlerFunc) http.Handler {
	return requireLevel(1, 1)(next)
}
