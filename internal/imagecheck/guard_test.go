package imagecheck

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/radiergummi/cetacean/internal/netguard"
)

// redirectingRegistry answers every manifest request with a redirect to target.
func redirectingRegistry(t *testing.T, target string) (*httptest.Server, Reference) {
	t.Helper()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)

	ref := Reference{Host: strings.TrimPrefix(server.URL, "https://"), Repository: "a", Tag: "1"}

	return server, ref
}

func TestResolveRefusesARedirectToPlainHTTP(t *testing.T) {
	var reached atomic.Bool
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached.Store(true)
	}))
	defer plain.Close()

	server, ref := redirectingRegistry(t, plain.URL+"/v2/a/manifests/1")

	if _, err := NewClient(server.Client()).Resolve(context.Background(), ref); err == nil {
		t.Error("Resolve followed a redirect from https to http")
	}
	if reached.Load() {
		t.Error("the plain-text target was requested")
	}
}

func TestResolveRefusesARedirectToAnInternalAddress(t *testing.T) {
	for _, target := range []string{
		"https://169.254.169.254/latest/meta-data/",
		"https://10.0.0.1/v2/a/manifests/1",
		"https://[::1]/v2/a/manifests/1",
	} {
		server, ref := redirectingRegistry(t, target)

		_, err := NewClient(server.Client()).Resolve(context.Background(), ref)
		if !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("redirect to %s: err = %v, want netguard.ErrBlocked", target, err)
		}
	}
}

func TestResolveRefusesARealmOnAnotherSite(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="https://auth.elsewhere.test/token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	ref := Reference{Host: strings.TrimPrefix(server.URL, "https://"), Repository: "a", Tag: "1"}

	_, err := NewClient(server.Client()).Resolve(context.Background(), ref)
	if !errors.Is(err, errForeignRealm) {
		t.Errorf("err = %v, want errForeignRealm", err)
	}
}

func TestSameSite(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"auth.docker.io", "registry-1.docker.io", true},
		{"ghcr.io", "ghcr.io:443", true},
		{"gitlab.example.com", "registry.example.com", true},
		{"GHCR.io", "ghcr.io", true},
		{"ghcr.io", "lscr.io", false},
		{"attacker.github.io", "victim.github.io", false},
		{"10.0.0.5", "10.1.0.5", false},
		{"10.0.0.5", "10.0.0.5:5000", true},
		{"registry.lan", "auth.lan", false},
	} {
		if got := sameSite(tc.a, tc.b); got != tc.want {
			t.Errorf("sameSite(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// The registry the service names may sit on the LAN; a host it hands the
// client to elsewhere may not, and no host may reach link-local.
func TestScreenAdmitsPrivateAddressesOnlyOnTheRegistrysSite(t *testing.T) {
	ctx := withRegistry(context.Background(), "registry.example.com:5000")

	for _, tc := range []struct {
		host string
		ip   string
		ok   bool
	}{
		{"registry.example.com", "10.0.0.5", true},
		{"auth.example.com", "10.0.0.6", true},
		{"registry.example.com", "169.254.169.254", false},
		{"elsewhere.test", "10.0.0.5", false},
		{"elsewhere.test", "127.0.0.1", false},
		{"elsewhere.test", "93.184.216.34", true},
	} {
		err := screen(ctx, tc.host, net.ParseIP(tc.ip))
		if (err == nil) != tc.ok {
			t.Errorf("screen(%s, %s) = %v, want allowed=%v", tc.host, tc.ip, err, tc.ok)
		}
	}
}

func TestGuardedTransportScreensTheDial(t *testing.T) {
	transport := guardedTransport()
	ctx := withRegistry(context.Background(), "registry.example.com")

	_, err := transport.DialContext(ctx, "tcp", "127.0.0.1:443")
	if !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("dial = %v, want netguard.ErrBlocked", err)
	}
}
