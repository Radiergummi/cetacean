package auth

import (
	"net/http"

	json "github.com/goccy/go-json"
)

// NoneProvider always returns a static anonymous identity.
type NoneProvider struct{}

var anonymousIdentity = &Identity{
	Subject:     "anonymous",
	DisplayName: "Anonymous",
	Provider:    "none",
}

func (p *NoneProvider) Authenticate(_ http.ResponseWriter, _ *http.Request) (*Identity, error) {
	return anonymousIdentity, nil
}

func (p *NoneProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/whoami", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anonymousIdentity)
	})
}
