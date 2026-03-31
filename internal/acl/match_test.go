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

		// Fix 8: Malformed resource strings against typed patterns.
		{"service:*", "nocolon", false},         // no colon in resource
		{"service:*", ":noname", false},          // empty type in resource
		{"service:*", "service:", true},           // path.Match("*","") = true; glob matches empty name
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

// Fix 5: Empty-subject identity matching.
func TestMatchAudience_EmptySubject(t *testing.T) {
	tests := []struct {
		name string
		expr string
		id   *auth.Identity
		want bool
	}{
		{
			name: "user:* with empty subject and empty email matches via subject (glob matches empty)",
			expr: "user:*",
			id:   &auth.Identity{Subject: "", Email: ""},
			want: true, // path.Match("*","") = true, so empty subject matches user:*
		},
		{
			name: "user:alice with empty subject does not match",
			expr: "user:alice",
			id:   &auth.Identity{Subject: ""},
			want: false,
		},
		{
			name: "user:* with empty subject but populated email matches via subject",
			expr: "user:*",
			id:   &auth.Identity{Subject: "", Email: "alice@example.com"},
			want: true, // matches on subject already (glob matches empty)
		},
		{
			name: "user:*@example.com with empty subject matches via email",
			expr: "user:*@example.com",
			id:   &auth.Identity{Subject: "", Email: "alice@example.com"},
			want: true, // subject doesn't match, but email does
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchAudience(tt.expr, tt.id)
			if got != tt.want {
				t.Errorf("matchAudience(%q, %+v) = %v, want %v", tt.expr, tt.id, got, tt.want)
			}
		})
	}
}
