package mcp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// TestOriginGuard verifies the MCP handler rejects forged Origin headers with
// 403 (DNS-rebinding defense required by the MCP Streamable HTTP transport)
// while letting allowed and empty origins through.
func TestOriginGuard(t *testing.T) {
	newHandler := func(allowed []string) http.Handler {
		srv := newToolTestServer(
			t,
			cache.New(nil),
			&fakeWriteClient{},
			config.OpsReadOnly,
			func(o *Options) {
				o.AllowedOrigins = allowed
			},
		)
		return srv.Handler()
	}

	post := func(h http.Handler, origin string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("forged origin is rejected", func(t *testing.T) {
		h := newHandler([]string{"https://good.example"})
		if got := post(h, "https://evil.example"); got != http.StatusForbidden {
			t.Errorf("forged origin: status = %d, want %d", got, http.StatusForbidden)
		}
	})

	t.Run("allowed origin passes", func(t *testing.T) {
		h := newHandler([]string{"https://good.example"})
		if got := post(h, "https://good.example"); got == http.StatusForbidden {
			t.Errorf("allowed origin: status = %d, want not 403", got)
		}
	})

	t.Run("missing origin passes", func(t *testing.T) {
		h := newHandler([]string{"https://good.example"})
		if got := post(h, ""); got == http.StatusForbidden {
			t.Errorf("missing origin: status = %d, want not 403", got)
		}
	})

	// Every MCP call is a POST that may write, and "*" is not an origin, so
	// a wildcard cannot let a cross-site page drive the tools.
	t.Run("wildcard does not admit a cross-site origin", func(t *testing.T) {
		h := newHandler([]string{"*"})
		if got := post(h, "https://anything.example"); got != http.StatusForbidden {
			t.Errorf("wildcard origin: status = %d, want %d", got, http.StatusForbidden)
		}
	})

	// A "*" among real origins is a literal list to api.CORSConfig.Wildcard(),
	// so it must be one here too: this guard used to read any "*" in the list
	// as a wildcard and wave the same configuration through.
	t.Run("a star among real origins is not a wildcard", func(t *testing.T) {
		h := newHandler([]string{"*", "https://good.example"})
		if got := post(h, "https://evil.example"); got != http.StatusForbidden {
			t.Errorf("star among origins: status = %d, want %d", got, http.StatusForbidden)
		}
	})
}

// TestStandardRequestHeadersPassOriginGuard ensures our DNS-rebinding guard
// does not interfere with the headers 2026-07-28 requires on every POST. It
// sends an allowed Origin so the guard actually runs — without one the guard
// short-circuits and the test would prove nothing.
func TestStandardRequestHeadersPassOriginGuard(t *testing.T) {
	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsReadOnly,
		func(o *Options) { o.AllowedOrigins = []string{"https://host.example"} },
	)

	req := modernRequest(t, 1, "tools/list", `{}`)
	req.Header.Set("Origin", "https://host.example")

	resp, env := sendMCP(t, srv.Handler(), req)
	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("origin guard rejected a well-formed 2026-07-28 request from an allowed origin")
	}

	if env.Error != nil {
		t.Fatalf("tools/list error: %+v", env.Error)
	}
}

// TestHostGuard covers a reverse proxy on the same host (tailscale serve, a
// sidecar) that dials 127.0.0.1 and keeps the public Host, beside a rebound
// name that reaches the same loopback listener.
func TestHostGuard(t *testing.T) {
	send := func(publicURL, host string) *httptest.ResponseRecorder {
		srv := newToolTestServer(
			t,
			cache.New(nil),
			&fakeWriteClient{},
			config.OpsReadOnly,
			func(o *Options) {
				o.PublicURL = publicURL
				o.AllowedHosts = []string{"swarm.lan"}
			},
		)

		req := modernRequest(t, 1, "tools/list", `{}`)
		req.Host = host
		req = req.WithContext(context.WithValue(
			req.Context(),
			http.LocalAddrContextKey,
			&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9000},
		))

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		return rec
	}

	for _, tc := range []struct {
		name, publicURL, host string
		want                  int
	}{
		{"public host passes", "https://swarm.example.ts.net", "swarm.example.ts.net", http.StatusOK},
		{"public host with a port passes", "https://Swarm.Example.ts.net", "swarm.example.ts.net:443", http.StatusOK},
		{"loopback host passes", "", "localhost:9000", http.StatusOK},
		{"an allowed host passes", "", "Swarm.lan:9000", http.StatusOK},
		{"foreign host is refused", "https://swarm.example.ts.net", "rebind.evil.example", http.StatusForbidden},
		{"no public URL refuses a foreign host", "", "swarm.example.ts.net", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rec := send(tc.publicURL, tc.host); rec.Code != tc.want {
				t.Errorf("status = %d, want %d, body %q", rec.Code, tc.want, rec.Body)
			}
		})
	}
}
