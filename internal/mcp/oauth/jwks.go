package oauth

import (
	"encoding/json"
	"net/http"

	jose "github.com/go-jose/go-jose/v4"
)

// jwksPath is where the key set is served. Not under /.well-known/: jwks.json
// is not a registered suffix (RFC 8615 §3), and a path ending in .json never
// reaches the mux — the negotiation middleware strips the extension first, so
// the SPA fallback would answer it.
const jwksPath = "/oauth/jwks"

// jwksMediaType is the media type RFC 7517 registers for a JWK Set.
const jwksMediaType = "application/jwk-set+json"

// HandleJWKS serves the public half of the access token signing key.
func (s *Server) HandleJWKS(w http.ResponseWriter, r *http.Request) {
	if s.keys == nil {
		http.Error(w, "no signing key", http.StatusServiceUnavailable)

		return
	}

	doc := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       &s.keys.signer.PublicKey,
		KeyID:     s.keys.kid,
		Algorithm: "ES256",
		Use:       "sig",
	}}}

	// Marshal first so an encoding failure doesn't write partial headers
	// followed by a 500 status.
	body, err := json.Marshal(doc)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", jwksMediaType)
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write(body)
}
