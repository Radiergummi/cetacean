package api

import (
	"net/http"
	"strconv"

	"github.com/radiergummi/cetacean/internal/config"
)

// requireLevel returns middleware that blocks requests when the configured
// operations level is below the required level for this endpoint.
func requireLevel(required, configured config.OperationsLevel) func(http.HandlerFunc) http.Handler {
	return func(next http.HandlerFunc) http.Handler {
		if configured >= required {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeErrorCode(w, r, "OPS001",
				"this operation requires operations level "+strconv.Itoa(int(required))+
					", but the server is configured at level "+strconv.Itoa(int(configured)))
		})
	}
}
