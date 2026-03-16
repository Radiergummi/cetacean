package auth

import (
	"context"
	"testing"
)

func TestContextRoundTrip(t *testing.T) {
	id := &Identity{
		Subject:     "user-123",
		DisplayName: "Test User",
		Email:       "test@example.com",
		Groups:      []string{"admin", "dev"},
		Provider:    "oidc",
		Raw:         map[string]any{"claim": "value"},
	}

	ctx := ContextWithIdentity(context.Background(), id)
	got := IdentityFromContext(ctx)

	if got == nil {
		t.Fatal("expected identity, got nil")
	}
	if got.Subject != id.Subject {
		t.Errorf("Subject = %q, want %q", got.Subject, id.Subject)
	}
	if got.DisplayName != id.DisplayName {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, id.DisplayName)
	}
	if got.Email != id.Email {
		t.Errorf("Email = %q, want %q", got.Email, id.Email)
	}
	if got.Provider != id.Provider {
		t.Errorf("Provider = %q, want %q", got.Provider, id.Provider)
	}
	if len(got.Groups) != len(id.Groups) {
		t.Errorf("Groups len = %d, want %d", len(got.Groups), len(id.Groups))
	}
	if got.Raw["claim"] != "value" {
		t.Errorf("Raw[claim] = %v, want %q", got.Raw["claim"], "value")
	}
	if got != id {
		t.Error("expected same pointer")
	}
}

func TestIdentityFromContext_Missing(t *testing.T) {
	got := IdentityFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}
