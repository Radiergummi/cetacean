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

// WriteIdentityFunc writes an authenticated identity to an HTTP response.
// Implementations control the wire format (e.g. plain JSON vs JSON-LD).
type WriteIdentityFunc func(http.ResponseWriter, *http.Request, *Identity)

// WhoamiHandler returns an http.HandlerFunc that authenticates via the
// provider and responds with the identity. Since /auth/* routes are exempt
// from the auth middleware, this handler calls Authenticate directly rather
// than reading identity from context. Callers pass writeIdentity to control
// the response format (e.g. JSON-LD wrapping); pass WriteIdentityJSON for
// plain JSON.
func WhoamiHandler(p Provider, writeIdentity WriteIdentityFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := p.Authenticate(w, r)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "AUT001", "authentication required")
			return
		}
		if id == nil {
			return // provider handled the response (e.g. redirect)
		}
		w.Header().Set("Cache-Control", "no-store")
		writeIdentity(w, r, id)
	}
}

// WriteIdentityJSON writes the identity as plain JSON. Used by auth provider
// tests; production code uses a JSON-LD writer instead.
func WriteIdentityJSON(w http.ResponseWriter, _ *http.Request, id *Identity) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(id) // best-effort: status already sent
}
