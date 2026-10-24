package cluster

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func gpuNode() swarm.Node {
	return swarm.Node{
		ID: "n1",
		Spec: swarm.NodeSpec{
			Role:        swarm.NodeRoleWorker,
			Annotations: swarm.Annotations{Labels: map[string]string{"gpu": "true", "zone": "eu"}},
		},
		Description: swarm.NodeDescription{
			Hostname: "worker-1",
			Platform: swarm.Platform{OS: "linux", Architecture: "x86_64"},
		},
	}
}

// The platform filter mirrors swarmkit's, because its whole purpose is to
// predict swarmkit's answer: an empty list runs anywhere, an empty field is a
// wildcard, "x86_64" and "amd64" are one architecture, and a node that reports
// no platform matches nothing a service actually asked for.
func TestNodeSupportsPlatform(t *testing.T) {
	node := func(os, arch string) swarm.Node {
		n := gpuNode()
		n.Description.Platform = swarm.Platform{OS: os, Architecture: arch}

		return n
	}

	cases := []struct {
		name      string
		node      swarm.Node
		platforms []swarm.Platform
		want      bool
	}{
		{"no platforms declared runs anywhere", node("linux", "amd64"), nil, true},
		{
			"exact match",
			node("linux", "arm64"),
			[]swarm.Platform{{OS: "linux", Architecture: "arm64"}},
			true,
		},
		{
			"architecture mismatch",
			node("linux", "amd64"),
			[]swarm.Platform{{OS: "linux", Architecture: "arm64"}},
			false,
		},
		{
			"os mismatch",
			node("windows", "amd64"),
			[]swarm.Platform{{OS: "linux", Architecture: "amd64"}},
			false,
		},
		{
			"empty architecture is a wildcard",
			node("linux", "s390x"),
			[]swarm.Platform{{OS: "linux"}},
			true,
		},
		{
			"empty os is a wildcard",
			node("windows", "amd64"),
			[]swarm.Platform{{Architecture: "amd64"}},
			true,
		},
		{
			"x86_64 is amd64",
			node("linux", "x86_64"),
			[]swarm.Platform{{OS: "linux", Architecture: "amd64"}},
			true,
		},
		{
			"aarch64 is arm64",
			node("linux", "aarch64"),
			[]swarm.Platform{{OS: "linux", Architecture: "arm64"}},
			true,
		},
		{
			// Blocking here would call every image in the cluster unplaceable
			// on this node over one field an old engine never published.
			"an unreported platform does not block",
			node("", ""),
			[]swarm.Platform{{OS: "linux", Architecture: "amd64"}},
			true,
		},
		{
			"attestation entries alone say nothing",
			node("linux", "aarch64"),
			[]swarm.Platform{{OS: "unknown", Architecture: "unknown"}},
			true,
		},
		{
			"any one entry is enough",
			node("linux", "arm64"),
			[]swarm.Platform{
				{OS: "linux", Architecture: "amd64"},
				{OS: "linux", Architecture: "arm64"},
			},
			true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := nodeSupportsPlatform(tc.node, tc.platforms)
			if ok != tc.want {
				t.Fatalf("ok = %v, want %v", ok, tc.want)
			}
			if !ok && reason == "" {
				t.Error("a refusal gave no reason")
			}
		})
	}
}

// A service with no placement section at all is placeable anywhere; nil must
// not be read as a filter nothing passes.
func TestNodeCanHostWithoutAPlacementSpec(t *testing.T) {
	if ok, _ := nodeCanHost(gpuNode(), nil, 0); !ok {
		t.Error("a service with no placement spec must be placeable anywhere")
	}
}

// The platform list is not hand-written: Swarm copies it from the image's
// manifest, so every service in a real cluster carries one. This is the exact
// spec Docker 29 recorded for `nginx:alpine` against a node reporting
// "aarch64" — the pairing that strands a multi-arch service on its own node.
func TestNodeSupportsPlatformOnARealMultiArchService(t *testing.T) {
	node := gpuNode()
	node.Description.Platform = swarm.Platform{OS: "linux", Architecture: "aarch64"}

	// Verbatim from `docker service inspect`, attestation placeholders included.
	nginxAlpine := []swarm.Platform{
		{Architecture: "amd64", OS: "linux"},
		{Architecture: "unknown", OS: "unknown"},
		{OS: "linux"},
		{Architecture: "unknown", OS: "unknown"},
		{Architecture: "arm64", OS: "linux"},
		{Architecture: "unknown", OS: "unknown"},
		{Architecture: "386", OS: "linux"},
		{Architecture: "unknown", OS: "unknown"},
		{Architecture: "ppc64le", OS: "linux"},
		{Architecture: "unknown", OS: "unknown"},
		{Architecture: "riscv64", OS: "linux"},
		{Architecture: "unknown", OS: "unknown"},
		{Architecture: "s390x", OS: "linux"},
		{Architecture: "unknown", OS: "unknown"},
	}

	if ok, reason := nodeSupportsPlatform(node, nginxAlpine); !ok {
		t.Fatalf("a service running on this very node was reported unplaceable: %s", reason)
	}

	// The same image without the architecture-wildcard entry the attestation
	// manifests contribute — nothing but explicit platforms, so the aarch64 /
	// arm64 fold is the only thing that can match it.
	explicit := []swarm.Platform{
		{Architecture: "amd64", OS: "linux"},
		{Architecture: "arm64", OS: "linux"},
	}

	if ok, reason := nodeSupportsPlatform(node, explicit); !ok {
		t.Fatalf("arm64 image reported unplaceable on an aarch64 node: %s", reason)
	}

	// And the check still has teeth: an image built only for another
	// architecture genuinely cannot run here.
	amdOnly := []swarm.Platform{{Architecture: "amd64", OS: "linux"}}

	ok, reason := nodeSupportsPlatform(node, amdOnly)
	if ok {
		t.Fatal("an amd64-only image was reported placeable on an arm64 node")
	}
	if !strings.Contains(reason, "linux/amd64") || !strings.Contains(reason, "linux/aarch64") {
		t.Errorf("reason = %q, want both sides named", reason)
	}
	if strings.Count(reason, "unknown") != 0 {
		t.Errorf("reason = %q, want the attestation entries left out", reason)
	}
}

// The node half of that sentence must not borrow the service half's wildcard
// vocabulary: a node that reports an architecture but no OS runs one specific
// thing Docker did not name, not "anything".
func TestNodeSupportsPlatformNamesAnUnreportedHalfAsUnknown(t *testing.T) {
	node := gpuNode()
	node.Description.Platform = swarm.Platform{Architecture: "arm64"}

	ok, reason := nodeSupportsPlatform(node, []swarm.Platform{{OS: "linux", Architecture: "amd64"}})
	if ok {
		t.Fatal("an amd64 image was reported placeable on an arm64 node")
	}
	if !strings.Contains(reason, "unknown/arm64") {
		t.Errorf(
			"reason = %q, want the unreported half named as unknown, not as a wildcard",
			reason,
		)
	}
}

// A node publishing one half of its platform and not the other must not be ruled
// out on the half it never stated. Requiring *both* halves to be absent before
// the guard stops blocking strands every service on a node reporting `OS: linux`
// with no architecture — a partial inspect, or an engine too old to publish one.
func TestNodeSupportsPlatformDoesNotBlockOnAHalfTheNodeNeverReported(t *testing.T) {
	cases := []struct {
		name string
		have swarm.Platform
	}{
		{"os without architecture", swarm.Platform{OS: "linux"}},
		{"architecture without os", swarm.Platform{Architecture: "amd64"}},
		{"neither", swarm.Platform{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := gpuNode()
			node.Description.Platform = tc.have

			ok, reason := nodeSupportsPlatform(node, []swarm.Platform{
				{OS: "linux", Architecture: "amd64"},
			})
			if !ok {
				t.Errorf("blocked on a half the node never reported: %s", reason)
			}
		})
	}
}
