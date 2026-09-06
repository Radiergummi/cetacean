package cluster

import (
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

// nodeCanHost reports whether a node passes every hard scheduling filter in a
// service's placement spec, and names the first one it fails.
//
// Swarm enforces three of them exactly — constraints, supported platforms, and
// the per-node replica cap — and a view that reads only the first reports a
// service movable onto nodes the scheduler will refuse, which is the one wrong
// answer the drain-impact view must not give: a service pinned to arm64, or
// capped at one replica per node, would be called movable onto a full amd64
// node and its replica would simply sit pending after the drain.
//
// placed is how many of this service's live tasks the node already carries,
// which is what the replica cap is measured against. Resource reservations are
// deliberately still not considered: unlike these three, spare capacity after a
// drain is a moving target, and the cluster status read already reports it.
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
// image supports, naming both sides when it is not — a caller told only that
// the platform is wrong cannot tell which way.
//
// Unlike a constraint, this list is not something an operator wrote: Swarm
// populates it from the image's manifest, so *every* service in a real cluster
// carries one. That inverts which mistake is affordable here. A constraint we
// cannot evaluate is reported unsatisfied, because a mistaken "movable" is the
// answer the drain-impact view must never give and an unparseable constraint
// is rare. Reading an unrecognised platform the same way would instead strand
// every service on the cluster the moment one field is spelled unexpectedly —
// so where this cannot tell, it does not block.
//
// Concretely each half is compared only when both sides state it: an empty
// field on the service's entry is a wildcard, and an empty field on the node's
// is one the engine did not report — neither is grounds to rule a node out,
// and treating an absent architecture as a mismatch would strand every service
// on that node. The comparison folds architecture aliases first: Docker's own
// node description says "aarch64" where a manifest says "arm64", and comparing
// those two literally strands every arm service on every arm node. An
// all-unknown entry is an attestation manifest rather than a platform, so it
// names nothing and is skipped.
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
// whose engine reports "aarch64" matches an image manifest declaring "arm64".
// The pairs are the ones containerd normalises, which is what actually resolves
// an image against a host; comparing the raw strings instead would report every
// arm service unplaceable on every arm node.
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
// Sharing describePlatform here produced the self-contradiction "runs any/any;
// the service supports linux/arm64" — a node said to run anything, in the
// sentence explaining why it cannot run this.
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
// service's spec, and names the first constraint that it does not.
//
// This is the rule the drain-impact view turns on: a service pinned to a label
// no remaining node carries has nowhere to go, and saying so is the whole
// answer to "if I drain this node, what moves?". Swarm holds the constraints
// and enforces them at schedule time, but nothing reads them back — the
// drain_node prompt currently instructs the model to compare them by eye
// across two listings.
//
// A constraint this cannot parse or does not recognise is reported as **not**
// satisfied, with the constraint as the reason. Reading an unevaluable
// constraint as satisfied would report a service movable that Swarm will
// refuse to place, which is the one wrong answer this view must not give.
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

// nodeAttribute resolves a constraint key against a node, reporting whether
// the key is one Swarm defines at all.
//
// The unknown-key answer is separate from the empty-value answer on purpose:
// `node.labels.gpu` on a node with no such label is a known key with no value
// (so `!=` holds), while `weird.key` is a key Swarm would reject outright and
// must not be quietly treated as unset.
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
