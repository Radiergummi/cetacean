package oauth

import (
	"net/http"

	jose "github.com/go-jose/go-jose/v4"
)

// Not /.well-known/jwks.json: that suffix is unregistered, and content
// negotiation strips a .json extension before routing, so such a path never
// reaches the mux.
const jwksPath = "/oauth/jwks"

const jwksMediaType = "application/jwk-set+json"

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

	writeDiscoveryDoc(w, doc, jwksMediaType)
}
