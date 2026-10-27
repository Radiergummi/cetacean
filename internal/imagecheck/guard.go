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

// withRegistry records the registry host a service names, which screen
// measures every other host against.
func withRegistry(ctx context.Context, host string) context.Context {
	return context.WithValue(ctx, registryKey{}, host)
}

// screen admits the registry's own site to private and loopback addresses, as
// a registry on the LAN is the operator's choice. A host the registry sends us
// to on another site gets the full screen; link-local is refused to everyone.
func screen(ctx context.Context, host string, ip net.IP) error {
	registry, _ := ctx.Value(registryKey{}).(string)
	own := registry != "" && sameSite(host, registry)

	return netguard.Policy{AllowLoopback: own, AllowPrivate: own}.Check(ip)
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

// checkRedirect follows only https, and screens an address literal the way
// the dialer would, which matters when a proxy does the dialling.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}

	if req.URL.Scheme != "https" {
		return fmt.Errorf("registry redirected to %s, not https", req.URL.Scheme)
	}

	if ip := net.ParseIP(req.URL.Hostname()); ip != nil {
		return screen(req.Context(), req.URL.Hostname(), ip)
	}

	return nil
}

// guardedTransport dials only screened addresses. A proxy configured in the
// environment is the operator's and is dialled unscreened.
func guardedTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	proxies := map[string]bool{}
	env := httpproxy.FromEnvironment()
	for _, proxy := range []string{env.HTTPSProxy, env.HTTPProxy} {
		if proxy == "" {
			continue
		}

		// ProxyFromEnvironment reads a value with no scheme as http.
		if !strings.Contains(proxy, "://") {
			proxy = "http://" + proxy
		}

		if u, err := url.Parse(proxy); err == nil {
			proxies[strings.ToLower(u.Hostname())] = true
		}
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = netguard.DialContext(
		dialer,
		func(ctx context.Context, host string, ip net.IP) error {
			if proxies[strings.ToLower(host)] {
				return nil
			}

			return screen(ctx, host, ip)
		},
	)

	return transport
}
