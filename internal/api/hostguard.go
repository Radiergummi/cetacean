package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/auth"
)

// hostGuard refuses a request whose Host hosts does not admit: the
// DNS-rebinding defence, since a rebound page is same-origin to the browser. A
// trusted proxy has routed by Host already, and probes that read no cluster
// state pass, as health checks name anything.
func hostGuard(hosts auth.HostSet) Constructor {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isProbe(r) || auth.FromTrustedProxy(r.Context()) || hosts.Admits(r.Host) {
				next.ServeHTTP(w, r)
				return
			}

			host := auth.HostName(r.Host)
			problem.Write(w, r, "CSR002", fmt.Sprintf(
				"this server does not answer for host %q; add it to server.allowed_hosts, "+
					"or set server.public_url to the URL it is reached at",
				host,
			))
		})
	}
}

// isProbe matches the meta endpoints a health check or scraper reaches by
// whatever name its network gives the container.
func isProbe(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	switch strings.TrimSuffix(r.URL.Path, "/") {
	case "/-/health", "/-/ready", "/-/metrics":
		return true
	default:
		return false
	}
}
