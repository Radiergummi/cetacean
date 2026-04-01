package acl

import (
	"log/slog"
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

	// Pattern uses glob matching. Errors indicate malformed patterns (e.g.,
	// unclosed '['), which should have been caught by policy validation. Log
	// and deny rather than silently ignoring.
	matched, err := path.Match(exprPattern, resName)
	if err != nil {
		slog.Warn("ACL glob match error", "pattern", exprPattern, "name", resName, "error", err)
		return false
	}
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
			if matched, err := path.Match(pattern, id.Subject); err != nil {
				slog.Warn(
					"ACL audience match error",
					"pattern",
					pattern,
					"subject",
					id.Subject,
					"error",
					err,
				)
			} else if matched {
				return true
			}
		}
		if id.Email != "" {
			if matched, err := path.Match(pattern, id.Email); err != nil {
				slog.Warn(
					"ACL audience match error",
					"pattern",
					pattern,
					"email",
					id.Email,
					"error",
					err,
				)
			} else if matched {
				return true
			}
		}
		return false
	case "group":
		for _, g := range id.Groups {
			if matched, err := path.Match(pattern, g); err != nil {
				slog.Warn("ACL audience match error", "pattern", pattern, "group", g, "error", err)
			} else if matched {
				return true
			}
		}
		return false
	default:
		return false
	}
}
