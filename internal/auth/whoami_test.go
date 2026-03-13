package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/goccy/go-json"
)

func TestHandleWhoami_Authenticated(t *testing.T) {
	id := &Identity{
		Subject:     "alice",
		DisplayName: "Alice",
		Provider:    "headers",
	}

	r := httptest.NewRequest(http.MethodGet, "/auth/whoami", nil)
	r = r.WithContext(ContextWithIdentity(r.Context(), id))
	w := httptest.NewRecorder()

	HandleWhoami(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var got Identity
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Subject != "alice" {
		t.Errorf("Subject = %q, want %q", got.Subject, "alice")
	}
	if got.Provider != "headers" {
		t.Errorf("Provider = %q, want %q", got.Provider, "headers")
	}
}

func TestHandleWhoami_Unauthenticated(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/auth/whoami", nil)
	w := httptest.NewRecorder()

	HandleWhoami(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
