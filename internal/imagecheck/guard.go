package imagecheck

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/publicsuffix"

	"github.com/radiergummi/cetacean/internal/netguard"
)

const maxRedirects = 10

type registryKey struct{}

// registry is the host a service names, and whether it sits on the LAN.
type registry struct {
	host string
	lan  bool
}

// lookupIP resolves a name for isLAN; a variable so tests need no DNS.
var lookupIP = net.DefaultResolver.LookupIP

// withRegistry records the registry a service names, which screen and the
// realm rule measure every other host against.
func withRegistry(ctx context.Context, host string) context.Context {
	return context.WithValue(ctx, registryKey{}, registry{host: host, lan: isLAN(ctx, host)})
}

func registryOf(ctx context.Context) registry {
	r, _ := ctx.Value(registryKey{}).(registry)
	return r
}

// isLAN reports whether host, or every address it resolves to, is private or
// loopback. A host that does not resolve is not.
func isLAN(ctx context.Context, host string) bool {
	host = hostOnly(host)

	ips := []net.IP{net.ParseIP(host)}
	if ips[0] == nil {
		var err error
		if ips, err = lookupIP(ctx, "ip", host); err != nil || len(ips) == 0 {
			return false
		}
	}

	for _, ip := range ips {
		if !netguard.Private(ip) && !ip.IsLoopback() {
			return false
		}
	}

	return true
}

// screen admits the registry's own site to private and loopback addresses, as
// a registry on the LAN is the operator's choice, and so is where it sends us.
// A public registry's hops elsewhere get the full screen; link-local never passes.
func screen(ctx context.Context, address string, ip net.IP) error {
	r := registryOf(ctx)
	own := r.host != "" && (r.lan || sameSite(address, r.host))

	return netguard.Policy{AllowLoopback: own, AllowPrivate: own}.Check(ip)
}

// realmAllowed admits a token realm on the site of the host that challenged,
// or anywhere on the LAN when the registry is on it too: Harbor by address
// with its realm at harbor.lan, or registry.lan with auth.lan.
func realmAllowed(ctx context.Context, realm, challenger string) bool {
	return sameSite(realm, challenger) || registryOf(ctx).lan && isLAN(ctx, realm)
}

// sameSite reports whether a and b, host or host:port, are one host or share a
// registrable domain: Docker Hub serves registry-1.docker.io and its tokens
// from auth.docker.io. IP literals match only themselves.
func sameSite(a, b string) bool {
	a, b = strings.ToLower(hostOnly(a)), strings.ToLower(hostOnly(b))
	if a == b {
		return true
	}

	if net.ParseIP(a) != nil || net.ParseIP(b) != nil {
		return false
	}

	siteA, errA := publicsuffix.EffectiveTLDPlusOne(a)
	siteB, errB := publicsuffix.EffectiveTLDPlusOne(b)

	return errA == nil && errB == nil && siteA == siteB
}

func hostOnly(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}

	return strings.Trim(hostport, "[]")
}

// redirectPolicy follows only https and screens an address literal the way
// the dialer would. Through a proxy no address can be screened, so a redirect
// must also stay on the registry's site.
func redirectPolicy(transport http.RoundTripper) func(*http.Request, []*http.Request) error {
	var proxy func(*http.Request) (*url.URL, error)
	if t, ok := transport.(*http.Transport); ok {
		proxy = t.Proxy
	}

	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}

		if req.URL.Scheme != "https" {
			return fmt.Errorf("registry redirected to %s, not https", req.URL.Scheme)
		}

		if proxy != nil {
			if through, err := proxy(req); err != nil || through != nil {
				if r := registryOf(req.Context()); !sameSite(req.URL.Host, r.host) {
					return fmt.Errorf(
						"%w: redirect to %s leaves %s through a proxy",
						netguard.ErrBlocked, req.URL.Host, r.host,
					)
				}
			}
		}

		if ip := net.ParseIP(req.URL.Hostname()); ip != nil {
			return screen(req.Context(), req.URL.Host, ip)
		}

		return nil
	}
}

// guardedTransport dials only screened addresses. The proxy env names is the
// operator's and is dialled unscreened, on its own port only.
func guardedTransport(env *httpproxy.Config) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxyFor := env.ProxyFunc()
	transport.Proxy = func(req *http.Request) (*url.URL, error) { return proxyFor(req.URL) }

	proxies := map[string]bool{}
	for _, proxy := range []string{env.HTTPSProxy, env.HTTPProxy} {
		if address := proxyAddress(proxy); address != "" {
			proxies[address] = true
		}
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = netguard.DialContext(
		dialer,
		func(ctx context.Context, address string, ip net.IP) error {
			if proxies[strings.ToLower(address)] {
				return nil
			}

			return screen(ctx, address, ip)
		},
	)

	return transport
}

// proxyAddress is the host:port a proxy setting dials, with its scheme's port
// when it names none. ProxyFromEnvironment reads a value with no scheme as http.
func proxyAddress(proxy string) string {
	if proxy == "" {
		return ""
	}

	if !strings.Contains(proxy, "://") {
		proxy = "http://" + proxy
	}

	u, err := url.Parse(proxy)
	if err != nil || u.Hostname() == "" {
		return ""
	}

	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443", "socks5": "1080"}[u.Scheme]
	}

	return strings.ToLower(net.JoinHostPort(u.Hostname(), port))
}
