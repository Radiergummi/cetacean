package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io/fs"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"testing/fstest"
	"time"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// newProxyRouter assembles the real router with the given auth provider and
// trusted-proxy set, exactly as main.go wires it: the same list reaches realIP
// and, through the trust verdict realIP records, the provider.
func newProxyRouter(t *testing.T, provider auth.Provider, trusted []netip.Prefix) http.Handler {
	t.Helper()

	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c))
	b := sse.NewBroadcaster(0, noopErrorWriter, nil)
	t.Cleanup(b.Close)

	fsys := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}

	return NewRouter(RouterConfig{
		Handlers:       h,
		Broadcaster:    b,
		SPA:            NewSPAHandler(fs.FS(fsys), ""),
		AuthProvider:   provider,
		TrustedProxies: trusted,
	})
}

// newProxyAuthRouter assembles the real router in headers auth mode, wiring
// the same trusted-proxy set to realIP and the provider as main.go does. The
// provider alone never sees a rewritten RemoteAddr, which is why headers mode
// broke behind a proxy while headers_test.go stayed green.
func newProxyAuthRouter(t *testing.T, trusted []netip.Prefix) http.Handler {
	t.Helper()

	return newProxyRouter(t, auth.NewHeadersProvider(config.HeadersConfig{
		Subject: "X-Auth-User",
	}), trusted)
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

// certRequest builds a GET /nodes carrying an RFC 9440 Client-Cert header, as
// a TLS-terminating proxy would forward it. r.TLS stays nil: the connection
// from the proxy is plain HTTP.
func certRequest(t *testing.T, peer string) *http.Request {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "alice"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Client-Cert", ":"+base64.StdEncoding.EncodeToString(der)+":")
	r.RemoteAddr = peer

	return r
}

// TestClientCertBehindTrustedProxy drives the assembled router in cert mode
// with no TLS of its own, asserting both directions: a certificate forwarded
// by a trusted proxy authenticates, the same header from anyone else does not.
func TestClientCertBehindTrustedProxy(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	router := newProxyRouter(t, &auth.CertProvider{}, trusted)

	tests := map[string]struct {
		peer   string
		status int
	}{
		"trusted proxy":  {peer: "10.0.0.5:1234", status: http.StatusOK},
		"untrusted peer": {peer: "203.0.113.9:1234", status: http.StatusUnauthorized},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, certRequest(t, tt.peer))

			if w.Code != tt.status {
				t.Fatalf("status=%d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}
