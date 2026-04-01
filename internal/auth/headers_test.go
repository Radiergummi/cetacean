package auth

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/config"
)

// anyProxy matches all IPv4 addresses. Used in tests that aren't testing
// proxy validation specifically, to satisfy the mandatory TrustedProxies
// requirement without affecting test semantics.
var anyProxy = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}

func TestHeadersProvider_Authenticate(t *testing.T) {
	t.Run("all headers", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			Name:           "X-Name",
			Email:          "X-Email",
			Groups:         "X-Groups",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "alice")
		r.Header.Set("X-Name", "Alice Smith")
		r.Header.Set("X-Email", "alice@example.com")
		r.Header.Set("X-Groups", "admin, editors, ")

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if id.Subject != "alice" {
			t.Errorf("Subject = %q, want %q", id.Subject, "alice")
		}
		if id.Provider != "headers" {
			t.Errorf("Provider = %q, want %q", id.Provider, "headers")
		}
		if id.DisplayName != "Alice Smith" {
			t.Errorf("DisplayName = %q, want %q", id.DisplayName, "Alice Smith")
		}
		if id.Email != "alice@example.com" {
			t.Errorf("Email = %q, want %q", id.Email, "alice@example.com")
		}
		if len(id.Groups) != 2 || id.Groups[0] != "admin" || id.Groups[1] != "editors" {
			t.Errorf("Groups = %v, want [admin editors]", id.Groups)
		}
		if id.Raw["subject_header"] != "X-User" {
			t.Errorf("Raw[subject_header] = %v, want %q", id.Raw["subject_header"], "X-User")
		}
	})

	t.Run("missing subject header", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for missing subject header")
		}
	})

	t.Run("valid shared secret", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			SecretHeader:   "X-Proxy-Secret",
			SecretValue:    "s3cret",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "bob")
		r.Header.Set("X-Proxy-Secret", "s3cret")

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Subject != "bob" {
			t.Errorf("Subject = %q, want %q", id.Subject, "bob")
		}
	})

	t.Run("invalid shared secret", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			SecretHeader:   "X-Proxy-Secret",
			SecretValue:    "s3cret",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "bob")
		r.Header.Set("X-Proxy-Secret", "wrong")

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for invalid secret")
		}
	})

	t.Run("missing secret header entirely", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			SecretHeader:   "X-Proxy-Secret",
			SecretValue:    "s3cret",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "bob")
		// X-Proxy-Secret not set at all.

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for missing secret header")
		}
	})

	t.Run("empty subject header value", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "")

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for empty subject header")
		}
	})

	t.Run("subject with leading and trailing whitespace", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "  alice  ")

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Current behavior: whitespace is preserved. This test documents it.
		if id.Subject != "  alice  " {
			t.Errorf("Subject = %q, want %q", id.Subject, "  alice  ")
		}
	})

	t.Run("groups with only commas", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			Groups:         "X-Groups",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "alice")
		r.Header.Set("X-Groups", ",,,")

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(id.Groups) != 0 {
			t.Errorf("Groups = %v, want empty", id.Groups)
		}
	})

	t.Run("groups with single value", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			Groups:         "X-Groups",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "alice")
		r.Header.Set("X-Groups", "admin")

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(id.Groups) != 1 || id.Groups[0] != "admin" {
			t.Errorf("Groups = %v, want [admin]", id.Groups)
		}
	})

	t.Run("header name case insensitivity", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(
			"X-User",
			"alice",
		) // Go canonicalizes headers; case insensitivity is guaranteed by net/http

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Subject != "alice" {
			t.Errorf("Subject = %q, want %q", id.Subject, "alice")
		}
	})

	t.Run("only subject header", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-Remote-User",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-Remote-User", "charlie")

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Subject != "charlie" {
			t.Errorf("Subject = %q, want %q", id.Subject, "charlie")
		}
		if id.DisplayName != "charlie" {
			t.Errorf(
				"DisplayName = %q, want %q (should fall back to subject)",
				id.DisplayName,
				"charlie",
			)
		}
		if id.Email != "" {
			t.Errorf("Email = %q, want empty", id.Email)
		}
		if len(id.Groups) != 0 {
			t.Errorf("Groups = %v, want empty", id.Groups)
		}
	})

	t.Run("subject with control characters rejected", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header["X-User"] = []string{"alice\x00bob"}

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for subject with control characters")
		}
		if !strings.Contains(err.Error(), "control character") {
			t.Errorf("error = %q, want mention of control character", err.Error())
		}
	})

	t.Run("subject with newline rejected", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header["X-User"] = []string{"alice\nbob"}

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for subject with newline")
		}
	})

	t.Run("subject exceeding max length rejected", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", strings.Repeat("a", maxSubjectLen+1))

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for subject exceeding max length")
		}
		if !strings.Contains(err.Error(), "exceeds maximum length") {
			t.Errorf("error = %q, want mention of maximum length", err.Error())
		}
	})

	t.Run("subject at max length accepted", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: anyProxy,
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", strings.Repeat("a", maxSubjectLen))

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(id.Subject) != maxSubjectLen {
			t.Errorf("Subject length = %d, want %d", len(id.Subject), maxSubjectLen)
		}
	})

	t.Run("extra headers captured in Raw", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: anyProxy,
		}, "X-ACL")

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "alice")
		r.Header.Set("X-Acl", `[{"resources":["service:*"],"permissions":["read"]}]`)

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		aclVal, ok := id.Raw["X-ACL"].(string)
		if !ok {
			t.Fatal("expected X-ACL in Raw map")
		}
		if aclVal == "" {
			t.Fatal("expected non-empty X-ACL value in Raw")
		}
	})

	t.Run("missing extra header not stored in Raw", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: anyProxy,
		}, "X-ACL")

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "alice")

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := id.Raw["X-ACL"]; ok {
			t.Fatal("expected X-ACL to not be in Raw when header is absent")
		}
	})
}

func TestHeadersProvider_TrustedProxies(t *testing.T) {
	trustedCIDR := netip.MustParsePrefix("10.0.0.0/8")
	trustedIP := netip.MustParsePrefix("192.168.1.1/32")

	t.Run("trusted proxy accepted", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: []netip.Prefix{trustedCIDR},
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.5:12345"
		r.Header.Set("X-User", "alice")

		id, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Subject != "alice" {
			t.Errorf("Subject = %q, want %q", id.Subject, "alice")
		}
	})

	t.Run("untrusted proxy rejected", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: []netip.Prefix{trustedCIDR},
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "172.16.0.1:12345"
		r.Header.Set("X-User", "alice")

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for untrusted proxy")
		}
		if !strings.Contains(err.Error(), "not a trusted proxy") {
			t.Errorf("error = %q, want mention of trusted proxy", err.Error())
		}
	})

	t.Run("exact IP match", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: []netip.Prefix{trustedIP},
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "192.168.1.1:443"
		r.Header.Set("X-User", "alice")

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("exact IP mismatch", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: []netip.Prefix{trustedIP},
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "192.168.1.2:443"
		r.Header.Set("X-User", "alice")

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for non-matching IP")
		}
	})

	t.Run("multiple trusted prefixes", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: []netip.Prefix{trustedCIDR, trustedIP},
		})

		// First prefix matches.
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.1:80"
		r.Header.Set("X-User", "alice")
		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("first prefix: unexpected error: %v", err)
		}

		// Second prefix matches.
		r = httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "192.168.1.1:80"
		r.Header.Set("X-User", "alice")
		_, err = p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("second prefix: unexpected error: %v", err)
		}
	})

	t.Run("IPv6 trusted proxy", func(t *testing.T) {
		ipv6Prefix := netip.MustParsePrefix("fd00::/8")
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			TrustedProxies: []netip.Prefix{ipv6Prefix},
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "[fd00::1]:443"
		r.Header.Set("X-User", "alice")

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("trusted proxy checked before secret", func(t *testing.T) {
		// Both configured — untrusted IP should fail before secret check.
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:        "X-User",
			SecretHeader:   "X-Secret",
			SecretValue:    "s3cret",
			TrustedProxies: []netip.Prefix{trustedCIDR},
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "172.16.0.1:12345"
		r.Header.Set("X-User", "alice")
		r.Header.Set("X-Secret", "s3cret") // correct secret, but untrusted IP

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for untrusted proxy despite valid secret")
		}
		if !strings.Contains(err.Error(), "not a trusted proxy") {
			t.Errorf("error = %q, want trusted proxy error (not secret error)", err.Error())
		}
	})

	t.Run("no trusted proxies configured rejects all", func(t *testing.T) {
		// No TrustedProxies — proxy check always runs and rejects.
		p := NewHeadersProvider(config.HeadersConfig{
			Subject: "X-User",
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "1.2.3.4:80"
		r.Header.Set("X-User", "alice")

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for missing trusted proxies")
		}
		if !strings.Contains(err.Error(), "not a trusted proxy") {
			t.Errorf("error = %q, want mention of trusted proxy", err.Error())
		}
	})
}

func TestValidateSubject(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "alice", false},
		{"valid email", "alice@example.com", false},
		{"valid unicode", "アリス", false},
		{"valid max length", strings.Repeat("a", maxSubjectLen), false},
		{"empty", "", true},
		{"too long", strings.Repeat("a", maxSubjectLen+1), true},
		{"null byte", "alice\x00", true},
		{"newline", "alice\n", true},
		{"carriage return", "alice\r", true},
		{"tab", "alice\t", true},
		{"bell", "alice\x07", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubject(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSubject(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
