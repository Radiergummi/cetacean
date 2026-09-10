package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"testing/fstest"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// newProxyAuthRouter assembles the real router in headers auth mode, wiring
// the same trusted-proxy set to realIP and the provider as main.go does. The
// provider alone never sees a rewritten RemoteAddr, which is why headers mode
// broke behind a proxy while headers_test.go stayed green.
func newProxyAuthRouter(t *testing.T, trusted []netip.Prefix) http.Handler {
	t.Helper()

	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c))
	b := sse.NewBroadcaster(0, noopErrorWriter, nil)
	t.Cleanup(b.Close)

	fsys := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}

	return NewRouter(RouterConfig{
		Handlers:    h,
		Broadcaster: b,
		SPA:         NewSPAHandler(fs.FS(fsys), ""),
		AuthProvider: auth.NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-Auth-User",
			TrustedProxies: trusted,
		}),
		TrustedProxies: trusted,
	})
}

// proxiedRequest builds a GET /nodes carrying the identity headers a reverse
// proxy would set, arriving from peer.
func proxiedRequest(peer string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.Header.Set("Accept", "application/json")
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.Header.Set("X-Auth-User", "alice")
	r.RemoteAddr = peer

	return r
}

// TestHeadersAuthBehindTrustedProxy: realIP has rewritten RemoteAddr to the
// client address by the time the provider runs, and auth still succeeds.
func TestHeadersAuthBehindTrustedProxy(t *testing.T) {
	router := newProxyAuthRouter(t, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, proxiedRequest("10.0.0.5:1234"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestHeadersAuthFromUntrustedPeer: the same headers sent directly by a client
// that is not a trusted proxy must be rejected.
func TestHeadersAuthFromUntrustedPeer(t *testing.T) {
	router := newProxyAuthRouter(t, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, proxiedRequest("203.0.113.9:1234"))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401; body=%s", w.Code, w.Body.String())
	}
}
