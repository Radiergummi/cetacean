package acl

import (
	"log/slog"
	"strings"
	"sync"

	"github.com/radiergummi/cetacean/internal/auth"
)

const (
	// LabelRead is the Docker label key for read audience grants.
	LabelRead = "cetacean.acl.read"
	// LabelWrite is the Docker label key for write audience grants.
	LabelWrite = "cetacean.acl.write"
)

// hasACLLabels returns true if the label map contains any cetacean.acl.* key.
func hasACLLabels(labels map[string]string) bool {
	_, hasRead := labels[LabelRead]
	_, hasWrite := labels[LabelWrite]
	return hasRead || hasWrite
}

// ParseACLLabels extracts read and write audience lists from labels.
// Returns nil, nil if no ACL labels are present.
func ParseACLLabels(labels map[string]string) (read, write []string) {
	return ParseAudienceList(labels[LabelRead]), ParseAudienceList(labels[LabelWrite])
}

// warnedAudiences holds the invalid expressions already reported. Parsing runs
// on every decision a label takes part in, so each is reported once.
var warnedAudiences sync.Map

// ParseAudienceList splits a comma-separated audience string, trims whitespace,
// and drops empty entries. Invalid expressions are included but logged as warnings.
func ParseAudienceList(value string) []string {
	parts := strings.Split(value, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Warn on invalid audience expressions but include them — matchAudience
		// will reject them at evaluation time.
		if err := validateAudience(p); err != nil {
			if _, warned := warnedAudiences.LoadOrStore(p, struct{}{}); !warned {
				slog.Warn("invalid audience expression in ACL label", "expression", p, "error", err)
			}
		}

		result = append(result, p)
	}
	return result
}

// matchLabelAudience returns true if id matches any of the given audience expressions.
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
