package auth

import "net/http"

// NoneProvider always returns a static anonymous identity.
type NoneProvider struct{}

func (p *NoneProvider) Authenticate(_ http.ResponseWriter, _ *http.Request) (*Identity, error) {
	return &Identity{
		Subject:     "anonymous",
		DisplayName: "Anonymous",
		Provider:    "none",
	}, nil
}

func (p *NoneProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/whoami", WhoamiHandler(p))
}
