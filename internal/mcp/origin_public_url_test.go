package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// A deployment naming only server.public_url serves a browser client at that
// origin, as the API's cross-origin guard does, and no other port or scheme.
func TestOriginGuardAdmitsThePublicURL(t *testing.T) {
	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsReadOnly,
		func(o *Options) { o.PublicURL = "https://cetacean.example:8443" },
	)
	h := srv.Handler()

	post := func(origin string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		return rec.Code
	}

	if got := post("https://cetacean.example:8443"); got == http.StatusForbidden {
		t.Errorf("own origin: status = %d, want not 403", got)
	}

	for _, origin := range []string{
		"https://cetacean.example",
		"http://cetacean.example:8443",
		"https://evil.example",
	} {
		if got := post(origin); got != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", origin, got)
		}
	}
}

// RFC 6454 compares origins on the scheme and host case-insensitively and on
// the port with its default filled in, for the configured side and the
// request's alike.
func TestOriginGuardComparesSerialisedOrigins(t *testing.T) {
	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsReadOnly,
		func(o *Options) {
			o.PublicURL = "HTTPS://Cetacean.Example:443"
			o.AllowedOrigins = []string{"http://Dash.Example:80", "http://[::1]:9000"}
		},
	)
	h := srv.Handler()

	post := func(origin string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		return rec.Code
	}

	for _, origin := range []string{
		"https://cetacean.example",
		"https://CETACEAN.example:443",
		"http://dash.example",
		"HTTP://dash.example:80",
		"http://[::1]:9000",
	} {
		if got := post(origin); got == http.StatusForbidden {
			t.Errorf("%s: status = %d, want admitted", origin, got)
		}
	}

	for _, origin := range []string{
		"https://cetacean.example:8443",
		"http://cetacean.example",
		"https://dash.example",
		"http://[::1]",
		"null",
	} {
		if got := post(origin); got != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", origin, got)
		}
	}
}
