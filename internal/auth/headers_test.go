package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/config"
)

func TestHeadersProvider_Authenticate(t *testing.T) {
	t.Run("all headers", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject: "X-User",
			Name:    "X-Name",
			Email:   "X-Email",
			Groups:  "X-Groups",
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
		if id.Raw["subject"] != "alice" {
			t.Errorf("Raw[subject] = %v, want %q", id.Raw["subject"], "alice")
		}
	})

	t.Run("missing subject header", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject: "X-User",
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for missing subject header")
		}
	})

	t.Run("valid shared secret", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject:      "X-User",
			SecretHeader: "X-Proxy-Secret",
			SecretValue:  "s3cret",
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
			Subject:      "X-User",
			SecretHeader: "X-Proxy-Secret",
			SecretValue:  "s3cret",
		})

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "bob")
		r.Header.Set("X-Proxy-Secret", "wrong")

		_, err := p.Authenticate(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected error for invalid secret")
		}
	})

	t.Run("only subject header", func(t *testing.T) {
		p := NewHeadersProvider(config.HeadersConfig{
			Subject: "X-Remote-User",
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
			t.Errorf("DisplayName = %q, want %q (should fall back to subject)", id.DisplayName, "charlie")
		}
		if id.Email != "" {
			t.Errorf("Email = %q, want empty", id.Email)
		}
		if len(id.Groups) != 0 {
			t.Errorf("Groups = %v, want empty", id.Groups)
		}
	})
}
