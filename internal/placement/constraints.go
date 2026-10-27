// Package placement evaluates Swarm placement constraints against a node. It
// is a leaf so the cache and the domain layer can share one reading of them.
package placement

import (
	"net"
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

		var matches bool
		if strings.EqualFold(key, "node.ip") {
			var valid bool
			if matches, valid = addressMatches(node.Status.Addr, value); !valid {
				return false, strings.TrimSpace(raw)
			}
		} else {
			actual, known := nodeAttribute(node, key)
			if !known {
				return false, strings.TrimSpace(raw)
			}

			// An absent label is unset rather than empty: it equals nothing and
			// differs from everything. Values compare without case, as Swarm's do.
			matches = strings.EqualFold(actual, value)
		}

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
// while `weird.key` is one Swarm rejects outright. Only a label's name has case.
func nodeAttribute(node swarm.Node, key string) (string, bool) {
	if label, found := cutPrefixFold(key, "node.labels."); found {
		return node.Spec.Labels[label], label != ""
	}

	if label, found := cutPrefixFold(key, "engine.labels."); found {
		return node.Description.Engine.Labels[label], label != ""
	}

	switch strings.ToLower(key) {
	case "node.id":
		return node.ID, true
	case "node.hostname":
		return node.Description.Hostname, true
	case "node.role":
		// Swarm reads the role the node holds, which the Engine API shows
		// only as ManagerStatus; Spec.Role is the one asked of it.
		if node.ManagerStatus != nil {
			return string(swarm.NodeRoleManager), true
		}

		return string(swarm.NodeRoleWorker), true
	case "node.platform.os":
		return node.Description.Platform.OS, true
	case "node.platform.arch":
		return node.Description.Platform.Architecture, true
	default:
		return "", false
	}
}

// addressMatches reports whether addr is the address value names, or lies in
// the network it names. A value that is neither is invalid: Swarm then fails
// the constraint whichever the operator.
func addressMatches(addr, value string) (matches, valid bool) {
	nodeIP := net.ParseIP(addr)

	if ip := net.ParseIP(value); ip != nil {
		return ip.Equal(nodeIP), true
	}

	if _, subnet, err := net.ParseCIDR(value); err == nil {
		return subnet.Contains(nodeIP), true
	}

	return false, false
}

// cutPrefixFold is strings.CutPrefix with the prefix matched without case.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}

	return s[len(prefix):], true
}
