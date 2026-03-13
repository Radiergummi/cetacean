package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tsnet"
)

// WhoIsClient abstracts the Tailscale WhoIs API for testing.
type WhoIsClient interface {
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
}

// TailscaleProvider authenticates requests using the local Tailscale daemon's
// WhoIs API to identify connecting peers by their remote address.
type TailscaleProvider struct {
	client WhoIsClient
}

// NewTailscaleTsnetProvider creates a provider using an embedded tsnet node.
// Returns the provider, the tsnet server (caller must close), and a net.Listener
// that should be used for serving authenticated routes.
func NewTailscaleTsnetProvider(hostname, authKey, stateDir string) (*TailscaleProvider, *tsnet.Server, net.Listener, error) {
	srv := &tsnet.Server{
		Hostname: hostname,
		AuthKey:  authKey,
		Dir:      stateDir,
	}

	ln, err := srv.Listen("tcp", ":443")
	if err != nil {
		srv.Close()
		return nil, nil, nil, fmt.Errorf("tsnet listen: %w", err)
	}

	lc, err := srv.LocalClient()
	if err != nil {
		ln.Close()
		srv.Close()
		return nil, nil, nil, fmt.Errorf("tsnet local client: %w", err)
	}

	return &TailscaleProvider{client: lc}, srv, ln, nil
}

// NewTailscaleLocalProvider creates a TailscaleProvider that uses the local
// Tailscale daemon to identify peers.
func NewTailscaleLocalProvider() *TailscaleProvider {
	return &TailscaleProvider{client: &local.Client{}}
}

// Authenticate identifies the Tailscale peer by calling WhoIs on the request's
// remote address. Returns an error if the caller is not on the tailnet.
func (p *TailscaleProvider) Authenticate(_ http.ResponseWriter, r *http.Request) (*Identity, error) {
	who, err := p.client.WhoIs(r.Context(), r.RemoteAddr)
	if err != nil {
		return nil, fmt.Errorf("tailscale whois: %w", err)
	}

	return &Identity{
		Subject:     fmt.Sprintf("%d", who.UserProfile.ID),
		DisplayName: who.UserProfile.DisplayName,
		Email:       who.UserProfile.LoginName,
		Provider:    "tailscale",
		Raw: map[string]any{
			"user_id":    int64(who.UserProfile.ID),
			"login_name": who.UserProfile.LoginName,
			"node_name":  who.Node.Name,
		},
	}, nil
}

func (p *TailscaleProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/whoami", WhoamiHandler(p))
}
