package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// xForwarded is realIP in the default mode.
func xForwarded(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return realIP(trusted, config.XForwardedHeaders)
}

func TestRealIP_NoTrustedProxies(t *testing.T) {
	handler := xForwarded(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "203.0.113.1:12345" {
			t.Errorf("RemoteAddr changed unexpectedly: %s", r.RemoteAddr)
		}
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.1:12345"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	handler.ServeHTTP(httptest.NewRecorder(), r)
}

func TestRealIP_UntrustedPeer(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	handler := xForwarded(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "203.0.113.1:12345" {
			t.Errorf("RemoteAddr changed for untrusted peer: %s", r.RemoteAddr)
		}
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.1:12345"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	handler.ServeHTTP(httptest.NewRecorder(), r)
}

func TestRealIP_TrustedPeer(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	handler := xForwarded(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "198.51.100.1:54321" {
			t.Errorf("expected real client IP, got %s", r.RemoteAddr)
		}
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	handler.ServeHTTP(httptest.NewRecorder(), r)
}

func TestRealIP_MultiHopChain(t *testing.T) {
	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
	}
	handler := xForwarded(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "203.0.113.50:9999" {
			t.Errorf("expected rightmost non-trusted IP, got %s", r.RemoteAddr)
		}
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	// Client → proxy1 (trusted 172.16.x) → proxy2 (trusted 10.x) → server
	r.Header.Set("X-Forwarded-For", "203.0.113.50, 172.16.0.5, 10.0.0.2")
	handler.ServeHTTP(httptest.NewRecorder(), r)
}

func TestRealIP_NoXFF(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	handler := xForwarded(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "10.0.0.1:12345" {
			t.Errorf("RemoteAddr changed without XFF: %s", r.RemoteAddr)
		}
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:12345"
	handler.ServeHTTP(httptest.NewRecorder(), r)
}

func TestRealIP_AllTrustedInXFF(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	handler := xForwarded(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "10.0.0.1:12345" {
			t.Errorf("RemoteAddr changed when all XFF entries are trusted: %s", r.RemoteAddr)
		}
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:12345"
	r.Header.Set("X-Forwarded-For", "10.0.0.2, 10.0.0.3")
	handler.ServeHTTP(httptest.NewRecorder(), r)
}

func TestRealIP_PreservesPort(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	handler := xForwarded(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "203.0.113.1:8080" {
			t.Errorf("expected port preserved from peer, got %s", r.RemoteAddr)
		}
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:8080"
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	handler.ServeHTTP(httptest.NewRecorder(), r)
}

func TestRealIP_IPv6(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("fd00::/8")}
	handler := xForwarded(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "[2001:db8::1]:443" {
			t.Errorf("expected IPv6 client, got %s", r.RemoteAddr)
		}
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[fd00::1]:443"
	r.Header.Set("X-Forwarded-For", "2001:db8::1")
	handler.ServeHTTP(httptest.NewRecorder(), r)
}

// TestRealIP_RecordsVerdictOnOriginalPeer: the verdict describes the peer the
// connection came from, not the client address the same middleware then
// writes into RemoteAddr.
func TestRealIP_RecordsVerdictOnOriginalPeer(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	called := false
	handler := xForwarded(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true

		peer, ok := auth.PeerFromContext(r.Context())
		if !ok {
			t.Fatal("no peer recorded in context")
		}
		if peer.Addr.String() != "10.0.0.1" {
			t.Errorf("peer address = %s, want the original peer 10.0.0.1", peer.Addr)
		}
		if !peer.Trusted {
			t.Error("peer not marked trusted")
		}
		if r.RemoteAddr != "203.0.113.1:54321" {
			t.Errorf("RemoteAddr = %s, want the client address", r.RemoteAddr)
		}
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if !called {
		t.Fatal("the wrapped handler never ran, so nothing was asserted")
	}
}

// TestRealIP_RecordsUntrustedVerdict: every way of failing the check — none
// configured, peer outside the set, unparseable peer — still records a
// verdict, so downstream code can tell "untrusted" from "undecided" and refuse
// the latter.
func TestRealIP_RecordsUntrustedVerdict(t *testing.T) {
	tests := map[string]struct {
		trusted    []netip.Prefix
		remoteAddr string
	}{
		"none configured": {remoteAddr: "203.0.113.1:12345"},
		"peer outside set": {
			trusted:    []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
			remoteAddr: "203.0.113.1:12345",
		},
		"unparseable peer": {
			trusted:    []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
			remoteAddr: "@",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			called := false
			inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				called = true

				peer, ok := auth.PeerFromContext(r.Context())
				if !ok {
					t.Fatal("no peer recorded in context")
				}

				if peer.Trusted {
					t.Errorf("peer %s marked trusted", peer.Addr)
				}
			})

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			r.Header.Set("X-Forwarded-For", "198.51.100.1")
			xForwarded(tt.trusted)(inner).ServeHTTP(httptest.NewRecorder(), r)

			if !called {
				t.Fatal("the wrapped handler never ran, so nothing was asserted")
			}
		})
	}
}

// TestRealIP_ClientResolution covers what RemoteAddr is rewritten to under
// each forwarding header family. The peer is always the trusted proxy
// 10.0.0.1:9999, so the port stays 9999; an empty want means RemoteAddr is
// left as it arrived.
func TestRealIP_ClientResolution(t *testing.T) {
	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
	}

	tests := []struct {
		name      string
		headers   config.ForwardedHeaders
		forwarded []string
		xff       string
		want      string
	}{
		{
			name:    "X-Forwarded-For names the client",
			headers: config.XForwardedHeaders,
			xff:     "203.0.113.50",
			want:    "203.0.113.50:9999",
		},
		{
			name:      "a client-supplied Forwarded cannot outrank the proxy's chain",
			headers:   config.XForwardedHeaders,
			forwarded: []string{"for=203.0.113.50"},
			xff:       "198.51.100.7",
			want:      "198.51.100.7:9999",
		},
		{
			name:      "a client-supplied Forwarded alone leaves the peer alone",
			headers:   config.XForwardedHeaders,
			forwarded: []string{"for=203.0.113.50"},
			want:      "",
		},
		{
			name:      "Forwarded names the client",
			headers:   config.RFC7239Headers,
			forwarded: []string{"for=203.0.113.50"},
			want:      "203.0.113.50:9999",
		},
		{
			name:      "a client-supplied X-Forwarded-For cannot outrank the proxy's chain",
			headers:   config.RFC7239Headers,
			forwarded: []string{"for=203.0.113.50"},
			xff:       "198.51.100.7",
			want:      "203.0.113.50:9999",
		},
		{
			name:    "a client-supplied X-Forwarded-For alone leaves the peer alone",
			headers: config.RFC7239Headers,
			xff:     "198.51.100.7",
			want:    "",
		},
		{
			name:    "Forwarded chain walks right to left past trusted hops",
			headers: config.RFC7239Headers,
			forwarded: []string{
				`for=203.0.113.50;proto=https, for="172.16.0.5:4711", for=10.0.0.2`,
			},
			want: "203.0.113.50:9999",
		},
		{
			name:      "quoted IPv6 with port",
			headers:   config.RFC7239Headers,
			forwarded: []string{`for="[2001:db8::1]:8080", for=10.0.0.2`},
			want:      "[2001:db8::1]:9999",
		},
		{
			name:      "an anonymised hop is read past",
			headers:   config.RFC7239Headers,
			forwarded: []string{"for=203.0.113.50, for=unknown, for=_hidden"},
			want:      "203.0.113.50:9999",
		},
		{
			name:      "Forwarded carrying no for parameter discloses no client",
			headers:   config.RFC7239Headers,
			forwarded: []string{"proto=https;host=example.com"},
			xff:       "198.51.100.7",
			want:      "",
		},
		{
			name:      "Forwarded naming no address discloses no client",
			headers:   config.RFC7239Headers,
			forwarded: []string{"for=unknown, for=_hidden"},
			xff:       "198.51.100.7",
			want:      "",
		},
		{
			name:      "an IPv4-mapped hop is recognised as the trusted proxy it is",
			headers:   config.RFC7239Headers,
			forwarded: []string{"for=203.0.113.50, for=\"[::ffff:10.0.0.2]\""},
			want:      "203.0.113.50:9999",
		},
		{
			name:      "every Forwarded node trusted leaves the peer alone",
			headers:   config.RFC7239Headers,
			forwarded: []string{"for=10.0.0.2, for=172.16.0.5"},
			want:      "",
		},
		{
			name:    "no forwarding header leaves the peer alone",
			headers: config.XForwardedHeaders,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const peer = "10.0.0.1:9999"

			want := tt.want
			if want == "" {
				want = peer
			}

			inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				if r.RemoteAddr != want {
					t.Errorf("RemoteAddr = %s, want %s", r.RemoteAddr, want)
				}
			})

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = peer
			for _, v := range tt.forwarded {
				r.Header.Add("Forwarded", v)
			}
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}

			realIP(trusted, tt.headers)(inner).ServeHTTP(httptest.NewRecorder(), r)
		})
	}
}

// TestRealIP_DropsUnwrittenForwarding: the family the proxy does not write is
// gone by the time any handler runs, whatever the peer.
func TestRealIP_DropsUnwrittenForwarding(t *testing.T) {
	tests := map[string]struct {
		headers config.ForwardedHeaders
		gone    []string
		kept    string
	}{
		"x-forwarded drops Forwarded": {
			headers: config.XForwardedHeaders,
			gone:    []string{"Forwarded"},
			kept:    "X-Forwarded-For",
		},
		"forwarded drops the X-Forwarded family": {
			headers: config.RFC7239Headers,
			gone:    []string{"X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host"},
			kept:    "Forwarded",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			called := false
			inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				called = true

				for _, header := range tt.gone {
					if got := r.Header.Get(header); got != "" {
						t.Errorf("%s = %q, want it dropped", header, got)
					}
				}
				if r.Header.Get(tt.kept) == "" {
					t.Errorf("%s was dropped too", tt.kept)
				}
			})

			// Untrusted, because the drop does not consult the verdict.
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = "203.0.113.1:9999"
			r.Header.Set("Forwarded", "for=203.0.113.50;proto=https;host=evil.example")
			r.Header.Set("X-Forwarded-For", "198.51.100.7")
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("X-Forwarded-Host", "cetacean.example.com")

			trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
			realIP(trusted, tt.headers)(inner).ServeHTTP(httptest.NewRecorder(), r)

			if !called {
				t.Fatal("the wrapped handler never ran, so nothing was asserted")
			}
		})
	}
}
