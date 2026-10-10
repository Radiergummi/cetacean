package cluster

import (
	"strings"

	cerrdefs "github.com/containerd/errdefs"
)

// The engine reports a removal's precondition with whatever status its
// subsystem chose: a local network in use is a 403, and swarmkit's refusals
// (a network or config in use, an undrained node) all flatten to 400. These
// are the messages that mark them, matched where the class alone cannot.
var removalConflictMessages = map[string][]string{
	"config":  {"is in use by the following"},
	"network": {"has active endpoints", "is in use by", "depends on it"},
	"node":    {"is not down", "must be demoted"},
	"secret":  {"is in use by the following"},
	"volume":  {"volume is in use"},
}

// IsRemovalConflict reports whether err refused a removal because of the
// resource's state, which the caller can fix, rather than a failure.
func IsRemovalConflict(err error, resource string) bool {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		return true
	}

	message := err.Error()
	for _, marker := range removalConflictMessages[resource] {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

// RemovalConflictDetail is what a refused removal tells the caller instead of
// the engine's text, which names the containers or services in the way.
func RemovalConflictDetail(resource, key string) string {
	return resource + " " + key + " cannot be removed in its current state"
}
