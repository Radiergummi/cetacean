package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

func TestRealIP_NoTrustedProxies(t *testing.T) {
	handler := realIP(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
	handler := realIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
	handler := realIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
	handler := realIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
	handler := realIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
	handler := realIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
	handler := realIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
	handler := realIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
	handler := realIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
}

// TestRealIP_RecordsUntrustedVerdict: both ways of failing the check still
// record a verdict, so downstream code can tell "untrusted" from "undecided".
func TestRealIP_RecordsUntrustedVerdict(t *testing.T) {
	for name, trusted := range map[string][]netip.Prefix{
		"none configured":  nil,
		"peer outside set": {netip.MustParsePrefix("10.0.0.0/8")},
	} {
		t.Run(name, func(t *testing.T) {
			inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				peer, ok := auth.PeerFromContext(r.Context())
				if !ok {
					t.Fatal("no peer recorded in context")
				}
				if peer.Trusted {
					t.Errorf("peer %s marked trusted", peer.Addr)
				}
			})

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = "203.0.113.1:12345"
			r.Header.Set("X-Forwarded-For", "198.51.100.1")
			realIP(trusted)(inner).ServeHTTP(httptest.NewRecorder(), r)
		})
	}
}

// TestRealIP_ClientResolution covers what the peer's RemoteAddr is rewritten
// to across both forwarding headers. The peer is always the trusted proxy
// 10.0.0.1:9999, so the port stays 9999 throughout; an empty want means
// RemoteAddr is left as it arrived.
func TestRealIP_ClientResolution(t *testing.T) {
	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
	}

	tests := []struct {
		name      string
		forwarded []string
		xff       string
		want      string
	}{
		{
			name:      "Forwarded names the client",
			forwarded: []string{"for=203.0.113.50"},
			want:      "203.0.113.50:9999",
		},
		{
			name:      "Forwarded is preferred over X-Forwarded-For",
			forwarded: []string{"for=203.0.113.50"},
			xff:       "198.51.100.7",
			want:      "203.0.113.50:9999",
		},
		{
			name: "Forwarded chain walks right to left past trusted hops",
			forwarded: []string{
				`for=203.0.113.50;proto=https, for="172.16.0.5:4711", for=10.0.0.2`,
			},
			want: "203.0.113.50:9999",
		},
		{
			name:      "quoted IPv6 with port",
			forwarded: []string{`for="[2001:db8::1]:8080", for=10.0.0.2`},
			want:      "[2001:db8::1]:9999",
		},
		{
			name:      "an anonymised hop is read past",
			forwarded: []string{"for=203.0.113.50, for=unknown, for=_hidden"},
			want:      "203.0.113.50:9999",
		},
		{
			name:      "Forwarded carrying no for parameter falls back to X-Forwarded-For",
			forwarded: []string{"proto=https;host=example.com"},
			xff:       "198.51.100.7",
			want:      "198.51.100.7:9999",
		},
		{
			name:      "every Forwarded node trusted leaves the peer alone",
			forwarded: []string{"for=10.0.0.2, for=172.16.0.5"},
			want:      "",
		},
		{
			name: "no forwarding header leaves the peer alone",
			want: "",
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

			realIP(trusted)(inner).ServeHTTP(httptest.NewRecorder(), r)
		})
	}
}
