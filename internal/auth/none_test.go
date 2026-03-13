package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoneProvider_Authenticate(t *testing.T) {
	p := &NoneProvider{}
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == nil {
		t.Fatal("expected identity, got nil")
	}
	if id.Subject != "anonymous" {
		t.Errorf("Subject = %q, want %q", id.Subject, "anonymous")
	}
	if id.DisplayName != "Anonymous" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "Anonymous")
	}
	if id.Provider != "none" {
		t.Errorf("Provider = %q, want %q", id.Provider, "none")
	}
}

func TestNoneProvider_RegisterRoutes(t *testing.T) {
	p := &NoneProvider{}
	mux := http.NewServeMux()
	// Should not panic.
	p.RegisterRoutes(mux)
}
