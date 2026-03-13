package auth

import (
	"net/http"

	json "github.com/goccy/go-json"
)

// HandleWhoami returns the identity from the request context as JSON.
// Returns 401 if no identity is present.
func HandleWhoami(w http.ResponseWriter, r *http.Request) {
	id := IdentityFromContext(r.Context())
	if id == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(id)
}
