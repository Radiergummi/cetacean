package config

import (
	"testing"
)

func TestNormalizeBasePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"/", ""},
		{"cetacean", "/cetacean"},
		{"/cetacean", "/cetacean"},
		{"/cetacean/", "/cetacean"},
		{"cetacean/", "/cetacean"},
		{"/cetacean/dashboard", "/cetacean/dashboard"},
		{"cetacean/dashboard/", "/cetacean/dashboard"},
		{"//cetacean", "/cetacean"},
		{"/cetacean//sub", "/cetacean/sub"},
	}
	for _, tt := range tests {
		got := NormalizeBasePath(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeBasePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateBasePath(t *testing.T) {
	valid := []string{
		"",
		"/cetacean",
		"/cetacean/dashboard",
		"/a/b/c",
	}
	for _, bp := range valid {
		if err := ValidateBasePath(bp); err != nil {
			t.Errorf("ValidateBasePath(%q) returned unexpected error: %v", bp, err)
		}
	}

	invalid := []struct {
		input string
		desc  string
	}{
		{"/cetacean?foo=bar", "query string"},
		{"/cetacean#anchor", "fragment"},
		{"/cetacean//sub", "double slash"},
	}
	for _, tt := range invalid {
		if err := ValidateBasePath(tt.input); err == nil {
			t.Errorf("ValidateBasePath(%q) expected error for %s, got nil", tt.input, tt.desc)
		}
	}
}

func TestLoad_BasePath_Default(t *testing.T) {
	t.Setenv("CETACEAN_BASE_PATH", "")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BasePath != "" {
		t.Errorf("BasePath=%q, want empty string", cfg.BasePath)
	}
}

func TestLoad_BasePath_EnvVar(t *testing.T) {
	t.Setenv("CETACEAN_BASE_PATH", "cetacean")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BasePath != "/cetacean" {
		t.Errorf("BasePath=%q, want /cetacean", cfg.BasePath)
	}
}

func TestLoad_BasePath_FlagOverridesEnv(t *testing.T) {
	t.Setenv("CETACEAN_BASE_PATH", "/from-env")

	bp := "/from-flag"
	flags := &Flags{BasePath: &bp}

	cfg, err := Load(nil, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BasePath != "/from-flag" {
		t.Errorf("BasePath=%q, want /from-flag", cfg.BasePath)
	}
}

func TestLoad_BasePath_FileOverridesDefault(t *testing.T) {
	t.Setenv("CETACEAN_BASE_PATH", "")

	bp := "/from-file"
	fc := &fileConfig{
		Server: &fileServer{BasePath: &bp},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BasePath != "/from-file" {
		t.Errorf("BasePath=%q, want /from-file", cfg.BasePath)
	}
}

func TestLoad_BasePath_InvalidReturnsError(t *testing.T) {
	t.Setenv("CETACEAN_BASE_PATH", "/bad?query=string")

	_, err := Load(nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid base path, got nil")
	}
}
