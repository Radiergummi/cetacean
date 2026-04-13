package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/config"
)

func TestWhoamiHandler_ReturnsIdentityJSON(t *testing.T) {
	p := NewHeadersProvider(config.HeadersConfig{
		Subject:        "X-User",
		Name:           "X-Name",
		Email:          "X-Email",
		Groups:         "X-Groups",
		TrustedProxies: anyProxy,
	})

	handler := WhoamiHandler(p, WriteIdentityJSON)
	r := httptest.NewRequest(http.MethodGet, "/auth/whoami", nil)
	r.Header.Set("X-User", "alice")
	r.Header.Set("X-Name", "Alice Smith")
	r.Header.Set("X-Email", "alice@example.com")
	r.Header.Set("X-Groups", "admin, dev")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var id Identity
	if err := json.NewDecoder(w.Body).Decode(&id); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if id.Subject != "alice" {
		t.Errorf("Subject = %q, want %q", id.Subject, "alice")
	}
	if id.DisplayName != "Alice Smith" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "Alice Smith")
	}
	if id.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", id.Email, "alice@example.com")
	}
	if len(id.Groups) != 2 || id.Groups[0] != "admin" || id.Groups[1] != "dev" {
		t.Errorf("Groups = %v, want [admin dev]", id.Groups)
	}
	if id.Provider != "headers" {
		t.Errorf("Provider = %q, want %q", id.Provider, "headers")
	}
}

func TestWhoamiHandler_Returns401WithoutHeaders(t *testing.T) {
	p := NewHeadersProvider(config.HeadersConfig{
		Subject:        "X-User",
		TrustedProxies: anyProxy,
	})

	handler := WhoamiHandler(p, WriteIdentityJSON)
	r := httptest.NewRequest(http.MethodGet, "/auth/whoami", nil)
	// No X-User header set.

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestWhoamiHandler_ValidSecret(t *testing.T) {
	p := NewHeadersProvider(config.HeadersConfig{
		Subject:        "X-User",
		SecretHeader:   "X-Proxy-Secret",
		SecretValue:    "s3cret",
		TrustedProxies: anyProxy,
	})

	handler := WhoamiHandler(p, WriteIdentityJSON)
	r := httptest.NewRequest(http.MethodGet, "/auth/whoami", nil)
	r.Header.Set("X-User", "bob")
	r.Header.Set("X-Proxy-Secret", "s3cret")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var id Identity
	if err := json.NewDecoder(w.Body).Decode(&id); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if id.Subject != "bob" {
		t.Errorf("Subject = %q, want %q", id.Subject, "bob")
	}
}

func TestWhoamiHandler_InvalidSecret_Returns401(t *testing.T) {
	p := NewHeadersProvider(config.HeadersConfig{
		Subject:        "X-User",
		SecretHeader:   "X-Proxy-Secret",
		SecretValue:    "s3cret",
		TrustedProxies: anyProxy,
	})

	handler := WhoamiHandler(p, WriteIdentityJSON)
	r := httptest.NewRequest(http.MethodGet, "/auth/whoami", nil)
	r.Header.Set("X-User", "bob")
	r.Header.Set("X-Proxy-Secret", "wrong")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestWhoamiHandler_SetsCacheControlNoStore(t *testing.T) {
	p := NewHeadersProvider(config.HeadersConfig{
		Subject:        "X-User",
		TrustedProxies: anyProxy,
	})

	handler := WhoamiHandler(p, WriteIdentityJSON)
	r := httptest.NewRequest(http.MethodGet, "/auth/whoami", nil)
	r.Header.Set("X-User", "alice")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}
