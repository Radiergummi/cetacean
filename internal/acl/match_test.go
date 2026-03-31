package acl

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

func TestMatchResource(t *testing.T) {
	tests := []struct {
		expr     string
		resource string
		want     bool
	}{
		{"*", "service:foo", true},
		{"service:foo", "service:foo", true},
		{"service:foo", "service:bar", false},
		{"service:*", "service:foo", true},
		{"service:webapp-*", "service:webapp-api", true},
		{"service:webapp-*", "service:other", false},
		{"node:orb-*", "node:orb-1", true},
		{"node:orb-*", "node:other", false},
		{"stack:monitoring", "stack:monitoring", true},
		{"stack:monitoring", "stack:other", false},
		{"*:*", "service:foo", true},
	}
	for _, tt := range tests {
		t.Run(tt.expr+"_"+tt.resource, func(t *testing.T) {
			if got := matchResource(tt.expr, tt.resource); got != tt.want {
				t.Errorf("matchResource(%q, %q) = %v, want %v", tt.expr, tt.resource, got, tt.want)
			}
		})
	}
}

func TestMatchAudience(t *testing.T) {
	alice := &auth.Identity{
		Subject: "alice",
		Email:   "alice@example.com",
		Groups:  []string{"engineering", "ops"},
	}

	tests := []struct {
		expr string
		want bool
	}{
		{"*", true},
		{"user:alice", true},
		{"user:bob", false},
		{"user:alice@example.com", true},
		{"user:*@example.com", true},
		{"group:engineering", true},
		{"group:ops", true},
		{"group:finance", false},
		{"group:eng*", true},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			if got := matchAudience(tt.expr, alice); got != tt.want {
				t.Errorf("matchAudience(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}
