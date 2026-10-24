package cluster

import (
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/placement"
)

// nodeCanHost reports whether a node passes every hard scheduling filter in a
// service's placement spec, naming the first it fails. Swarm enforces exactly
// three: constraints, supported platforms and the per-node replica cap, which
// placed is measured against. Resource reservations are deliberately not.
func nodeCanHost(node swarm.Node, spec *swarm.Placement, placed int) (bool, string) {
	if spec == nil {
		return true, ""
	}

	if ok, reason := placement.Satisfies(node, spec.Constraints); !ok {
		return false, reason
	}

	if ok, reason := nodeSupportsPlatform(node, spec.Platforms); !ok {
		return false, reason
	}

	if spec.MaxReplicas > 0 && uint64(placed) >= spec.MaxReplicas {
		return false, fmt.Sprintf(
			"already runs %d of at most %d replica(s) per node",
			placed, spec.MaxReplicas,
		)
	}

	return true, ""
}

// nodeSupportsPlatform reports whether a node's platform is one the service's
// image supports, naming both sides when not. This list comes from the image
// manifest, so an unrecognised value must not block — the reverse of
// placement.Satisfies' rule. Each half is compared only when both sides state it.
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
