package auth

import "net/http"

// Provider authenticates incoming HTTP requests and returns an Identity.
// Authenticate returns (nil, nil) if the provider handled the response itself
// (e.g. issued a redirect), or (nil, error) if authentication failed.
type Provider interface {
	Authenticate(w http.ResponseWriter, r *http.Request) (*Identity, error)
	RegisterRoutes(mux *http.ServeMux)
}
