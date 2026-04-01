package acl

import (
	"path"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
)

// matchResource checks whether a resource expression matches a resource
// string. resource is "type:name" (e.g. "service:webapp-api").
// expression is "type:pattern" with glob wildcards, or bare "*" for all.
func matchResource(expression, resource string) bool {
	if expression == "*" {
		return true
	}

	exprType, exprPattern, ok := strings.Cut(expression, ":")
	if !ok {
		return false
	}
	resType, resName, ok := strings.Cut(resource, ":")
	if !ok {
		return false
	}

	// Type must match exactly (or expression type is "*").
	if exprType != "*" && exprType != resType {
		return false
	}

	// Empty resource names never match any pattern.
	if resName == "" {
		return false
	}

	// Pattern uses glob matching.
	matched, _ := path.Match(exprPattern, resName)
	return matched
}

// matchAudience checks whether an audience expression matches an identity.
// "user:pattern" matches against Subject and Email (union).
// "group:pattern" matches against each entry in Groups.
// Bare "*" matches everyone.
func matchAudience(expression string, id *auth.Identity) bool {
	if expression == "*" {
		return true
	}

	kind, pattern, ok := strings.Cut(expression, ":")
	if !ok {
		return false
	}

	switch kind {
	case "user":
		if id.Subject != "" {
			if matched, _ := path.Match(pattern, id.Subject); matched {
				return true
			}
		}
		if id.Email != "" {
			if matched, _ := path.Match(pattern, id.Email); matched {
				return true
			}
		}
		return false
	case "group":
		for _, g := range id.Groups {
			if matched, _ := path.Match(pattern, g); matched {
				return true
			}
		}
		return false
	default:
		return false
	}
}
