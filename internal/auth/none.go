package auth

import "net/http"

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
	mux.HandleFunc("GET /auth/whoami", WhoamiHandler(p))
}
