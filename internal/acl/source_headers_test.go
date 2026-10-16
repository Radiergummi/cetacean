package acl

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// A request shaped as tailscale serve sends it, from 127.0.0.1 in the task's namespace.
func serveRequest(capabilities ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Tailscale-User-Login", "alice@example.com")
	r.Header.Set("Tailscale-User-Name", "Alice")
	for _, c := range capabilities {
		r.Header.Add("Tailscale-App-Capabilities", c)
	}

	return r.WithContext(auth.ContextWithPeer(r.Context(), auth.Peer{
		Addr:    netip.MustParseAddr("127.0.0.1"),
		Trusted: true,
	}))
}

func TestHeadersSourceReadsTailscaleServeCapabilities(t *testing.T) {
	const header = "Tailscale-App-Capabilities"
	provider := auth.NewHeadersProvider(config.HeadersConfig{
		Subject: "Tailscale-User-Login",
		Name:    "Tailscale-User-Name",
		Email:   "Tailscale-User-Login",
	}, header)
	src := &HeadersSource{Header: header, Capability: "example.com/cap/cetacean"}

	t.Run("captured and read", func(t *testing.T) {
		r := serveRequest(`{"example.com/cap/cetacean":[` +
			`{"resources":["*"],"permissions":["read"]},` +
			`{"resources":["service:*"],"permissions":["write"]}]}`)

		id, err := provider.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := id.Raw[header].(string); !ok {
			t.Fatalf("Raw[%s] not captured: %v", header, id.Raw)
		}
		if got := src.GrantsFor(id); len(got) != 2 {
			t.Errorf("GrantsFor() returned %d grants, want 2", len(got))
		}
	})

	t.Run("sent twice is refused", func(t *testing.T) {
		grant := `{"example.com/cap/cetacean":[{"resources":["*"],"permissions":["read"]}]}`
		r := serveRequest(grant, grant)

		if _, err := provider.Authenticate(httptest.NewRecorder(), r); err == nil {
			t.Error("a duplicated capabilities header was accepted")
		}
	})
}
