package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"

	json "github.com/goccy/go-json"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"
)

// tailscaleCGNAT is the Tailscale CGNAT range (100.64.0.0/10) used for IPv4
// tailnet addresses, plus the Tailscale IPv6 ULA prefix (fd7a:115c:a1e0::/48).
var (
	tailscaleCGNAT = netip.MustParsePrefix("100.64.0.0/10")
	tailscaleULA   = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// WhoIsClient abstracts the Tailscale WhoIs API for testing.
type WhoIsClient interface {
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
}

// TailscaleProvider authenticates requests using the local Tailscale daemon's
// WhoIs API to identify connecting peers by their remote address.
// If capability is set, groups are extracted from the matching app capability
// in the WhoIs response's CapMap (see Tailscale Application Capabilities).
type TailscaleProvider struct {
	client     WhoIsClient
	capability tailcfg.PeerCapability // app capability key for groups extraction
}

// NewTailscaleTsnetProvider creates a provider using an embedded tsnet node.
// Returns the provider, the tsnet server (caller must close), and a net.Listener
// that should be used for serving authenticated routes.
func NewTailscaleTsnetProvider(
	hostname, authKey, stateDir, capability string,
) (*TailscaleProvider, *tsnet.Server, net.Listener, error) {
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

	return &TailscaleProvider{
		client:     lc,
		capability: tailcfg.PeerCapability(capability),
	}, srv, ln, nil
}

// NewTailscaleLocalProvider creates a TailscaleProvider that uses the local
// Tailscale daemon to identify peers.
func NewTailscaleLocalProvider(capability string) *TailscaleProvider {
	return &TailscaleProvider{
		client:     &local.Client{},
		capability: tailcfg.PeerCapability(capability),
	}
}

// Authenticate identifies the Tailscale peer by calling WhoIs on the request's
// remote address. Returns an error if the caller is not on the tailnet.
// As defense-in-depth, the remote address is validated against the Tailscale
// CGNAT (100.64.0.0/10) and ULA (fd7a:115c:a1e0::/48) ranges before calling
// WhoIs, preventing spoofed non-tailnet addresses from reaching the daemon.
func (p *TailscaleProvider) Authenticate(
	_ http.ResponseWriter,
	r *http.Request,
) (*Identity, error) {
	if err := validateTailscaleAddr(r.RemoteAddr); err != nil {
		return nil, err
	}

	who, err := p.client.WhoIs(r.Context(), r.RemoteAddr)
	if err != nil {
		return nil, fmt.Errorf("tailscale whois: %w", err)
	}

	if who.UserProfile == nil {
		return nil, errors.New("tailscale: no user profile (tagged device?)")
	}
	if who.Node == nil {
		return nil, errors.New("tailscale: no node info")
	}

	id := &Identity{
		Subject:     fmt.Sprintf("%d", who.UserProfile.ID),
		DisplayName: who.UserProfile.DisplayName,
		Email:       who.UserProfile.LoginName,
		Provider:    "tailscale",
		Raw: map[string]any{
			"user_id":    int64(who.UserProfile.ID),
			"login_name": who.UserProfile.LoginName,
			"node_name":  who.Node.Name,
		},
	}

	if p.capability != "" {
		id.Groups = extractCapGroups(who.CapMap, p.capability)
	}

	return id, nil
}

func (p *TailscaleProvider) RegisterRoutes(_ *http.ServeMux) {}

// capGrantGroups is the JSON structure expected inside each capability grant
// value. Grants may contain a "groups" array of strings that map to the
// Identity.Groups field.
type capGrantGroups struct {
	Groups []string `json:"groups"`
}

// extractCapGroups reads all grant values for the given capability key from the
// CapMap and collects their "groups" arrays into a single deduplicated slice.
// Multiple grant rules may contribute groups (e.g. different ACL rules granting
// overlapping group sets).
func extractCapGroups(capMap tailcfg.PeerCapMap, capability tailcfg.PeerCapability) []string {
	values, ok := capMap[capability]
	if !ok || len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var groups []string
	for _, raw := range values {
		var grant capGrantGroups
		if err := json.Unmarshal([]byte(raw), &grant); err != nil {
			continue // skip malformed grant values
		}
		for _, g := range grant.Groups {
			if _, dup := seen[g]; !dup {
				seen[g] = struct{}{}
				groups = append(groups, g)
			}
		}
	}

	return groups
}

// validateTailscaleAddr checks that the remote address belongs to a Tailscale
// address range. This prevents non-tailnet traffic that arrives on a
// non-Tailscale interface (e.g. when the server binds to 0.0.0.0) from being
// passed to the WhoIs daemon.
func validateTailscaleAddr(remoteAddr string) error {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return errors.New("tailscale: invalid remote address")
	}

	ip, err := netip.ParseAddr(host)
	if err != nil {
		return errors.New("tailscale: invalid remote IP")
	}

	if !tailscaleCGNAT.Contains(ip) && !tailscaleULA.Contains(ip) {
		return fmt.Errorf("tailscale: remote address %s is not in tailnet range", ip)
	}

	return nil
}
