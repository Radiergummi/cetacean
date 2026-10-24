// Package placement evaluates Swarm placement constraints against a node. It
// is a leaf so the cache and the domain layer can share one reading of them.
package placement

import (
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

// Satisfies reports whether a node meets every placement constraint in a
// service's spec, naming the first it does not. A constraint it cannot parse
// or does not recognise is **not** satisfied: the other reading calls a service
// movable that Swarm will refuse to place.
func Satisfies(node swarm.Node, constraints []string) (bool, string) {
	for _, raw := range constraints {
		key, op, value, ok := splitConstraint(raw)
		if !ok {
			return false, strings.TrimSpace(raw)
		}

		actual, known := nodeAttribute(node, key)
		if !known {
			return false, strings.TrimSpace(raw)
		}

		// An absent label is unset rather than empty: it equals nothing and
		// differs from everything, which is how Swarm reads it too.
		matches := actual == value
		if op == "!=" {
			matches = !matches
		}

		if !matches {
			return false, strings.TrimSpace(raw)
		}
	}

	return true, ""
}

// splitConstraint parses `key==value` or `key!=value`, tolerating the
// whitespace a hand-written spec commonly carries. Docker supports only these
// two operators, so anything else is a constraint we cannot evaluate rather
// than one we evaluate wrongly.
func splitConstraint(raw string) (key, op, value string, ok bool) {
	for _, candidate := range []string{"==", "!="} {
		key, value, found := strings.Cut(raw, candidate)
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if key == "" || value == "" {
			return "", "", "", false
		}

		return key, candidate, value, true
	}

	return "", "", "", false
}

// nodeAttribute resolves a constraint key against a node, reporting whether the
// key is one Swarm defines at all. Unknown-key is separate from empty-value:
// `node.labels.gpu` on a node without it is known and unset, so `!=` holds,
// while `weird.key` is one Swarm rejects outright.
func nodeAttribute(node swarm.Node, key string) (string, bool) {
	if label, found := strings.CutPrefix(key, "node.labels."); found {
		return node.Spec.Labels[label], label != ""
	}

	if label, found := strings.CutPrefix(key, "engine.labels."); found {
		return node.Description.Engine.Labels[label], label != ""
	}

	switch key {
	case "node.id":
		return node.ID, true
	case "node.hostname":
		return node.Description.Hostname, true
	case "node.role":
		return string(node.Spec.Role), true
	case "node.platform.os":
		return node.Description.Platform.OS, true
	case "node.platform.arch":
		return node.Description.Platform.Architecture, true
	default:
		return "", false
	}
}
