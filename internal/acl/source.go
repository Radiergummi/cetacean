package acl

import (
	"github.com/radiergummi/cetacean/internal/auth"
)

// GrantSource provides per-user grants from an auth provider.
// Provider grants omit the audience field — they are implicitly scoped
// to the authenticated user.
type GrantSource interface {
	GrantsFor(id *auth.Identity) []Grant
}

// ResourceResolver resolves cross-resource relationships for ACL evaluation.
type ResourceResolver interface {
	// StackOf returns the stack name for a resource, or "" if it doesn't
	// belong to a stack.
	StackOf(resourceType, resourceID string) string

	// ServiceOfTask returns the service name for a task, or "" if unknown.
	ServiceOfTask(taskID string) string
}

// extractGrantsFromRaw parses a raw slice of grant-like maps into Grant
// structs. Used by provider grant sources to parse JSON from tokens/headers.
func extractGrantsFromRaw(raw []any) []Grant {
	var grants []Grant
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		g := Grant{}
		if resources, ok := m["resources"].([]any); ok {
			for _, r := range resources {
				if s, ok := r.(string); ok {
					g.Resources = append(g.Resources, s)
				}
			}
		}
		if perms, ok := m["permissions"].([]any); ok {
			for _, p := range perms {
				if s, ok := p.(string); ok {
					g.Permissions = append(g.Permissions, s)
				}
			}
		}
		if len(g.Resources) > 0 && len(g.Permissions) > 0 && validateGrant(g) == nil {
			grants = append(grants, g)
		}
	}
	return grants
}
