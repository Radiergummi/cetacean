package cluster

import (
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

// nodeCanHost reports whether a node passes every hard scheduling filter in a
// service's placement spec, naming the first it fails. Swarm enforces exactly
// three: constraints, supported platforms and the per-node replica cap, which
// placed is measured against. Resource reservations are deliberately not.
func nodeCanHost(node swarm.Node, placement *swarm.Placement, placed int) (bool, string) {
	if placement == nil {
		return true, ""
	}

	if ok, reason := nodeSatisfies(node, placement.Constraints); !ok {
		return false, reason
	}

	if ok, reason := nodeSupportsPlatform(node, placement.Platforms); !ok {
		return false, reason
	}

	if placement.MaxReplicas > 0 && uint64(placed) >= placement.MaxReplicas {
		return false, fmt.Sprintf(
			"already runs %d of at most %d replica(s) per node",
			placed, placement.MaxReplicas,
		)
	}

	return true, ""
}

// nodeSupportsPlatform reports whether a node's platform is one the service's
// image supports, naming both sides when not. This list comes from the image
// manifest, so an unrecognised value must not block — the reverse of
// nodeSatisfies' rule. Each half is compared only when both sides state it.
func nodeSupportsPlatform(node swarm.Node, platforms []swarm.Platform) (bool, string) {
	if len(platforms) == 0 {
		return true, ""
	}

	have := node.Description.Platform
	haveArch := normalizeArch(have.Architecture)

	named := make([]string, 0, len(platforms))

	for _, want := range platforms {
		if isAttestationPlatform(want) {
			continue
		}

		named = append(named, describePlatform(want))

		archMatches := want.Architecture == "" || haveArch == "" ||
			normalizeArch(want.Architecture) == haveArch
		osMatches := want.OS == "" || have.OS == "" || want.OS == have.OS

		if archMatches && osMatches {
			return true, ""
		}
	}

	// A list of nothing but attestation entries says nothing about placement.
	if len(named) == 0 {
		return true, ""
	}

	return false, fmt.Sprintf(
		"runs %s; the service supports %s",
		describeNodePlatform(have), strings.Join(named, ", "),
	)
}

// isAttestationPlatform reports whether an entry is one of the "unknown/unknown"
// placeholders a manifest list carries for its attestation manifests. Docker
// copies them into the placement spec verbatim — a plain multi-arch image
// yields one per real platform — and they describe no node.
func isAttestationPlatform(p swarm.Platform) bool {
	return p.Architecture == "unknown" && p.OS == "unknown"
}

// normalizeArch folds the architecture aliases that mean one machine, so a node
// reporting "aarch64" matches a manifest declaring "arm64". The pairs are the
// ones containerd normalises, which is what resolves an image against a host.
func normalizeArch(arch string) string {
	switch arch {
	case "x86_64", "x86-64", "amd64":
		return "amd64"
	case "aarch64", "arm64", "armv8", "armv8l":
		return "arm64"
	case "i386", "i686", "x86", "386":
		return "386"
	case "armv7l", "armv7", "armhf", "arm":
		return "arm"
	default:
		return arch
	}
}

// describePlatform renders a platform the way a Docker image reference does,
// naming an unspecified half rather than leaving a bare slash. An empty field
// on a *service's* entry is a wildcard, so it reads as "any".
func describePlatform(p swarm.Platform) string {
	return renderPlatform(p, "any")
}

// describeNodePlatform renders the same pair for the node side, where an empty
// field means the engine did not report one rather than "anything goes".
// Sharing describePlatform gives "runs any/any" in the sentence explaining why
// the node cannot run this service.
func describeNodePlatform(p swarm.Platform) string {
	return renderPlatform(p, "unknown")
}

func renderPlatform(p swarm.Platform, absent string) string {
	os, arch := p.OS, p.Architecture
	if os == "" {
		os = absent
	}
	if arch == "" {
		arch = absent
	}

	return os + "/" + arch
}

// nodeSatisfies reports whether a node meets every placement constraint in a
// service's spec, naming the first it does not. A constraint it cannot parse
// or does not recognise is **not** satisfied: the other reading calls a service
// movable that Swarm will refuse to place.
func nodeSatisfies(node swarm.Node, constraints []string) (bool, string) {
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
