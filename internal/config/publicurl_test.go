package config

import (
	"strings"
	"testing"
)

func TestValidatePublicURL(t *testing.T) {
	valid := []string{
		"",
		"https://cetacean.example.com",
		"http://localhost:9000",
		"https://cetacean.example.com:8443",
		"http://10.0.0.5:9000",
	}

	for _, raw := range valid {
		if err := ValidatePublicURL(raw); err != nil {
			t.Errorf("ValidatePublicURL(%q) = %v, want nil", raw, err)
		}
	}

	invalid := []struct {
		raw     string
		wantSub string
	}{
		{"ftp://cetacean.example.com", "http or https"},
		{"cetacean.example.com", "http or https"},
		{"https://", "must include a host"},
		{"http://:9000", "must include a host"},
		{"https://cetacean.example.com/cetacean", "server.base_path"},
		{"https://cetacean.example.com?a=b", "query or fragment"},
		{"https://cetacean.example.com#frag", "query or fragment"},
		{"https://user:pass@cetacean.example.com", "userinfo"},
	}

	for _, tt := range invalid {
		err := ValidatePublicURL(tt.raw)
		if err == nil {
			t.Errorf("ValidatePublicURL(%q) = nil, want error", tt.raw)
			continue
		}
		if !strings.Contains(err.Error(), tt.wantSub) {
			t.Errorf("ValidatePublicURL(%q) = %q, want it to mention %q", tt.raw, err, tt.wantSub)
		}
	}
}

func TestLoadPublicURLStripsTrailingSlash(t *testing.T) {
	t.Setenv("CETACEAN_PUBLIC_URL", "https://cetacean.example.com/")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.PublicURL != "https://cetacean.example.com" {
		t.Errorf("PublicURL = %q, want %q", cfg.PublicURL, "https://cetacean.example.com")
	}
}

func TestLoadRejectsPublicURLWithPath(t *testing.T) {
	t.Setenv("CETACEAN_PUBLIC_URL", "https://cetacean.example.com/cetacean")

	if _, err := Load(nil, nil); err == nil {
		t.Fatal("Load = nil error, want a rejection naming server.base_path")
	}
}
