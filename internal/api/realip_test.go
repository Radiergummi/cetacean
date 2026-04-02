package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
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
