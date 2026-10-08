//go:build e2e

// Package proxy provides a loopback reverse proxy the tests control
// completely, so they can emit the malformed and hostile traffic a
// well-behaved proxy never produces.
package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

// Start runs a reverse proxy in front of upstream, applying mutate to every
// outbound request. The SUT trusts 127.0.0.1, so whatever mutate writes is
// taken as coming from a trusted proxy.
func Start(t *testing.T, upstream string, mutate func(*http.Request)) string {
	t.Helper()

	target, err := url.Parse(upstream)
	if err != nil {
		t.Fatalf("parse upstream %q: %v", upstream, err)
	}

	// Rewrite, not the deprecated Director, is used here deliberately: it
	// still starts pr.Out as a clone of the inbound request (headers
	// included), so mutate can Add a genuinely duplicate header value that
	// reaches the SUT as two header lines rather than being collapsed.
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			mutate(pr.Out)
		},
	}

	server := httptest.NewServer(rp)
	t.Cleanup(server.Close)

	return server.URL
}
