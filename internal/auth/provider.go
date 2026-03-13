package auth

import (
	"net/http"

	json "github.com/goccy/go-json"
)

// Provider authenticates incoming HTTP requests and returns an Identity.
// Authenticate returns (nil, nil) if the provider handled the response itself
// (e.g. issued a redirect), or (nil, error) if authentication failed.
type Provider interface {
	Authenticate(w http.ResponseWriter, r *http.Request) (*Identity, error)
	RegisterRoutes(mux *http.ServeMux)
}

// WhoamiHandler returns an http.HandlerFunc that authenticates via the
// provider and responds with the identity as JSON. Since /auth/* routes
// are exempt from the auth middleware, this handler calls Authenticate
// directly rather than reading identity from context.
func WhoamiHandler(p Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := p.Authenticate(w, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if id == nil {
			return // provider handled the response (e.g. redirect)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(id)
	}
}
