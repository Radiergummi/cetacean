package api

import (
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
)

// hostGuard refuses a request whose Host names none of ours: the DNS-rebinding
// defence, since a rebound page is same-origin to the browser. An IP literal
// cannot be rebound and loopback is ours; a trusted proxy has routed by Host
// already. Probes that read no cluster state pass, as health checks name anything.
func hostGuard(publicURL string, allowed []string) Constructor {
	if u, err := url.Parse(publicURL); publicURL != "" && err == nil {
		allowed = append(slices.Clone(allowed), u.Host)
	}

	names := make([]string, 0, len(allowed))
	for _, name := range allowed {
		if name = auth.HostName(name); name != "" {
			names = append(names, strings.ToLower(name))
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := auth.HostName(r.Host)

			if isProbe(r) || auth.FromTrustedProxy(r.Context()) || auth.IsLoopbackHost(host) ||
				isIPLiteral(host) || slices.Contains(names, strings.ToLower(host)) {
				next.ServeHTTP(w, r)
				return
			}

			writeErrorCode(w, r, "CSR002", "this server does not answer for host "+r.Host)
		})
	}
}

func isIPLiteral(host string) bool {
	_, err := netip.ParseAddr(host)
	return err == nil
}

// isProbe matches the meta endpoints a health check or scraper reaches by
// whatever name its network gives the container.
func isProbe(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	switch r.URL.Path {
	case "/-/health", "/-/ready", "/-/metrics":
		return true
	default:
		return false
	}
}
