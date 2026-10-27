package imagecheck

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/net/http/httpproxy"

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

// stubLookup answers lookupIP from names for the rest of the test.
func stubLookup(t *testing.T, names map[string]string) {
	t.Helper()

	previous := lookupIP
	lookupIP = func(_ context.Context, _, host string) ([]net.IP, error) {
		if ip, ok := names[host]; ok {
			return []net.IP{net.ParseIP(ip)}, nil
		}

		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	t.Cleanup(func() { lookupIP = previous })
}

// A registry on loopback is on the LAN, which may send the client to other LAN
// hosts, but never to link-local.
func TestResolveRefusesARedirectToLinkLocal(t *testing.T) {
	server, ref := redirectingRegistry(t, "https://169.254.169.254/latest/meta-data/")

	_, err := NewClient(server.Client()).Resolve(context.Background(), ref)
	if !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("err = %v, want netguard.ErrBlocked", err)
	}
}

func TestRedirectPolicyScreensAPublicRegistrysHops(t *testing.T) {
	stubLookup(t, map[string]string{"registry.example.com": "93.184.216.34"})
	ctx := withRegistry(context.Background(), "registry.example.com")
	policy := redirectPolicy(&http.Transport{})

	for target, ok := range map[string]bool{
		"https://10.0.0.1/v2/a/manifests/1":         false,
		"https://[::1]/v2/a/manifests/1":            false,
		"https://169.254.169.254/":                  false,
		"https://93.184.216.40/v2/a/manifests/1":    true,
		"https://cdn.elsewhere.test/v2/a/manifests": true,
	} {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err := policy(req, nil); (err == nil) != ok {
			t.Errorf("redirect to %s: %v, want allowed=%v", target, err, ok)
		}
	}
}

// Through a proxy the dialer never sees the registry's addresses, so a
// redirect may not leave the registry's site at all.
func TestRedirectPolicyKeepsAProxiedRedirectOnTheRegistrysSite(t *testing.T) {
	stubLookup(t, map[string]string{"registry-1.docker.io": "93.184.216.34"})
	ctx := withRegistry(context.Background(), "registry-1.docker.io")

	proxy, _ := url.Parse("http://proxy.corp:3128")
	policy := redirectPolicy(&http.Transport{Proxy: http.ProxyURL(proxy)})

	for target, ok := range map[string]bool{
		"https://auth.docker.io/token":           true,
		"https://cdn.elsewhere.test/v2/manifest": false,
	} {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err := policy(req, nil); (err == nil) != ok {
			t.Errorf("proxied redirect to %s: %v, want allowed=%v", target, err, ok)
		}
	}
}

func TestResolveRefusesARealmOnAnotherSite(t *testing.T) {
	// A public registry, so the LAN rule cannot admit the realm.
	stubLookup(t, map[string]string{
		"auth.elsewhere.test": "10.0.0.9",
		"public.test":         "93.184.216.34",
	})
	ctx := withRegistry(context.Background(), "public.test")

	_, err := NewClient(http.DefaultClient).fetchToken(
		ctx,
		challenge{
			scheme: "bearer",
			params: map[string]string{"realm": "https://auth.elsewhere.test/token"},
		},
		Reference{Host: "public.test", Repository: "a", Tag: "1"},
		"public.test",
	)
	if !errors.Is(err, errForeignRealm) {
		t.Errorf("err = %v, want errForeignRealm", err)
	}
}

// Harbor reached by address names its realm by its LAN name, and a registry
// on one LAN name may keep its token service on another.
func TestRealmAllowedOnTheLANWhenTheRegistryIsOnIt(t *testing.T) {
	stubLookup(t, map[string]string{
		"harbor.lan":   "10.0.0.5",
		"registry.lan": "192.168.1.10",
		"auth.lan":     "192.168.1.11",
		"public.test":  "93.184.216.34",
	})

	for _, tc := range []struct {
		registry, realm string
		want            bool
	}{
		{"10.0.0.5", "harbor.lan", true},
		{"registry.lan:5000", "auth.lan", true},
		{"registry.lan", "public.test", false},
		{"public.test", "harbor.lan", false},
		{"public.test", "10.0.0.5", false},
		{"registry.lan", "unresolvable.lan", false},
	} {
		ctx := withRegistry(context.Background(), tc.registry)
		if got := realmAllowed(ctx, tc.realm, tc.registry); got != tc.want {
			t.Errorf("realm %s for %s: allowed=%v, want %v", tc.realm, tc.registry, got, tc.want)
		}
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

// A public registry's own site may sit on the LAN; a host it hands the client
// to elsewhere may not, and no host may reach link-local.
func TestScreenAdmitsPrivateAddressesOnlyOnTheRegistrysSite(t *testing.T) {
	stubLookup(t, map[string]string{"registry.example.com": "93.184.216.34"})
	ctx := withRegistry(context.Background(), "registry.example.com:5000")

	for _, tc := range []struct {
		address string
		ip      string
		ok      bool
	}{
		{"registry.example.com:5000", "10.0.0.5", true},
		{"auth.example.com:443", "10.0.0.6", true},
		{"registry.example.com:5000", "169.254.169.254", false},
		{"elsewhere.test:443", "10.0.0.5", false},
		{"elsewhere.test:443", "127.0.0.1", false},
		{"elsewhere.test:443", "93.184.216.34", true},
	} {
		err := screen(ctx, tc.address, net.ParseIP(tc.ip))
		if (err == nil) != tc.ok {
			t.Errorf("screen(%s, %s) = %v, want allowed=%v", tc.address, tc.ip, err, tc.ok)
		}
	}
}

func TestGuardedTransportScreensTheDial(t *testing.T) {
	stubLookup(t, map[string]string{"registry.example.com": "93.184.216.34"})
	transport := guardedTransport(&httpproxy.Config{})
	ctx := withRegistry(context.Background(), "registry.example.com")

	_, err := transport.DialContext(ctx, "tcp", "127.0.0.1:443")
	if !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("dial = %v, want netguard.ErrBlocked", err)
	}
}

// The proxy is exempt on the port it was configured with, not on every port
// of its host.
func TestGuardedTransportExemptsOnlyTheProxysPort(t *testing.T) {
	stubLookup(t, map[string]string{"registry.example.com": "93.184.216.34"})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	transport := guardedTransport(
		&httpproxy.Config{HTTPSProxy: "http://" + listener.Addr().String()},
	)
	ctx := withRegistry(context.Background(), "registry.example.com")

	conn, err := transport.DialContext(ctx, "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dialling the proxy: %v", err)
	}
	_ = conn.Close()

	if _, err := transport.DialContext(ctx, "tcp", "127.0.0.1:1"); !errors.Is(
		err,
		netguard.ErrBlocked,
	) {
		t.Errorf("dialling another port on the proxy's host = %v, want netguard.ErrBlocked", err)
	}
}

func TestProxyAddress(t *testing.T) {
	for proxy, want := range map[string]string{
		"":                   "",
		"proxy.corp:3128":    "proxy.corp:3128",
		"http://Proxy.corp":  "proxy.corp:80",
		"https://proxy.corp": "proxy.corp:443",
		"socks5://[fd00::1]": "[fd00::1]:1080",
	} {
		if got := proxyAddress(proxy); got != want {
			t.Errorf("proxyAddress(%q) = %q, want %q", proxy, got, want)
		}
	}
}
