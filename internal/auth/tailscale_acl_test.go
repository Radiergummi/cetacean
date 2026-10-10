package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
)

type stubWhoIs struct{ result *apitype.WhoIsResponse }

func (s stubWhoIs) WhoIs(context.Context, string) (*apitype.WhoIsResponse, error) {
	return s.result, nil
}

// The ACL grant source reads the peer's capabilities from the identity the
// real provider builds, not from a hand-assembled one.
func TestTailscaleCapabilityGrantsReachTheACLSource(t *testing.T) {
	const capability = "example.com/cap/cetacean"

	provider := auth.NewTailscaleProviderWithClient(stubWhoIs{&apitype.WhoIsResponse{
		UserProfile: &tailcfg.UserProfile{ID: 7, LoginName: "alice@example.com"},
		Node:        &tailcfg.Node{Name: "laptop."},
		CapMap: tailcfg.PeerCapMap{
			capability: {
				`{"resources":["service:web-*"],"permissions":["read"]}`,
				`{"resources":["stack:*"],"permissions":["read","write"]}`,
			},
			"example.com/cap/other": {`{"resources":["*"],"permissions":["write"]}`},
		},
	}})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "100.64.0.1:12345"

	id, err := provider.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	grants := (&acl.TailscaleSource{Capability: capability}).GrantsFor(id)
	if len(grants) != 2 {
		t.Fatalf("grants = %+v, want the two under %s", grants, capability)
	}

	if grants[1].Resources[0] != "stack:*" || len(grants[1].Permissions) != 2 {
		t.Errorf("second grant = %+v", grants[1])
	}
}
