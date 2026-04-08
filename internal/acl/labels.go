package acl

import (
	"log/slog"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
)

const (
	labelRead  = "cetacean.acl.read"
	labelWrite = "cetacean.acl.write"
)

// hasACLLabels returns true if the label map contains any cetacean.acl.* key.
func hasACLLabels(labels map[string]string) bool {
	_, hasRead := labels[labelRead]
	_, hasWrite := labels[labelWrite]
	return hasRead || hasWrite
}

// parseACLLabels extracts read and write audience lists from labels.
// Returns nil, nil if no ACL labels are present.
func parseACLLabels(labels map[string]string) (read, write []string) {
	readVal, hasRead := labels[labelRead]
	writeVal, hasWrite := labels[labelWrite]

	if !hasRead && !hasWrite {
		return nil, nil
	}

	if hasRead {
		read = parseAudienceList(readVal)
	}
	if hasWrite {
		write = parseAudienceList(writeVal)
	}
	return read, write
}

// parseAudienceList splits a comma-separated audience string, trims whitespace,
// and drops empty entries.
func parseAudienceList(value string) []string {
	parts := strings.Split(value, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Warn on invalid audience expressions but include them — matchAudience
		// will reject them at evaluation time.
		if p != "*" {
			kind, _, ok := strings.Cut(p, ":")
			if !ok || (kind != "user" && kind != "group") {
				slog.Warn("invalid audience expression in ACL label", "expression", p)
			}
		}

		result = append(result, p)
	}
	return result
}

// matchLabelAudience checks if any audience expression matches the identity.
// Reuses the existing matchAudience function from match.go.
func matchLabelAudience(audiences []string, id *auth.Identity) bool {
	if id == nil {
		return false
	}
	for _, expr := range audiences {
		if matchAudience(expr, id) {
			return true
		}
	}
	return false
}
