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

// The constraint form Swarm actually enforces, and the one drain-impact turns
// on: a service pinned to a label no remaining node carries has nowhere to go.
func TestNodeSatisfiesLabelEquality(t *testing.T) {
	ok, reason := nodeSatisfies(gpuNode(), []string{"node.labels.gpu==true"})
	if !ok {
		t.Errorf("ok = false for a node carrying the label: %s", reason)
	}

	ok, reason = nodeSatisfies(gpuNode(), []string{"node.labels.gpu==false"})
	if ok {
		t.Error("ok = true for a node whose label does not match")
	}
	if reason == "" {
		t.Error("a refusal must name the constraint that blocked it")
	}
}

func TestNodeSatisfiesInequality(t *testing.T) {
	if ok, _ := nodeSatisfies(gpuNode(), []string{"node.labels.zone!=us"}); !ok {
		t.Error("ok = false: zone is eu, so != us holds")
	}
	if ok, _ := nodeSatisfies(gpuNode(), []string{"node.labels.zone!=eu"}); ok {
		t.Error("ok = true: zone is eu, so != eu must not hold")
	}
}

// A label that is absent is not equal to anything, and is unequal to
// everything — the rule Swarm follows, and the one that decides whether an
// unlabelled node is a candidate.
func TestNodeSatisfiesTreatsAnAbsentLabelAsUnset(t *testing.T) {
	bare := swarm.Node{ID: "n2", Description: swarm.NodeDescription{Hostname: "worker-2"}}

	if ok, _ := nodeSatisfies(bare, []string{"node.labels.gpu==true"}); ok {
		t.Error("ok = true for a node with no such label")
	}
	if ok, _ := nodeSatisfies(bare, []string{"node.labels.gpu!=true"}); !ok {
		t.Error("ok = false: an unset label is unequal to any value")
	}
}

func TestNodeSatisfiesBuiltInKeys(t *testing.T) {
	node := gpuNode()

	for _, tc := range []struct {
		constraint string
		want       bool
	}{
		{"node.id==n1", true},
		{"node.id==n9", false},
		{"node.hostname==worker-1", true},
		{"node.role==worker", true},
		{"node.role==manager", false},
		{"node.platform.os==linux", true},
		{"node.platform.arch==x86_64", true},
	} {
		if ok, _ := nodeSatisfies(node, []string{tc.constraint}); ok != tc.want {
			t.Errorf("%s = %v, want %v", tc.constraint, ok, tc.want)
		}
	}
}

// Engine labels are a separate namespace from node labels: the daemon sets
// them, an operator sets the others, and a service may pin either.
func TestNodeSatisfiesEngineLabels(t *testing.T) {
	node := gpuNode()
	node.Description.Engine.Labels = map[string]string{"storage": "ssd"}

	if ok, _ := nodeSatisfies(node, []string{"engine.labels.storage==ssd"}); !ok {
		t.Error("ok = false for a matching engine label")
	}
	if ok, _ := nodeSatisfies(node, []string{"node.labels.storage==ssd"}); ok {
		t.Error("an engine label must not satisfy a node.labels constraint")
	}
}

// Every constraint must hold, and the reason names the first that did not —
// a caller fixing placement fixes one constraint at a time.
func TestNodeSatisfiesRequiresEveryConstraint(t *testing.T) {
	ok, reason := nodeSatisfies(
		gpuNode(),
		[]string{"node.labels.gpu==true", "node.labels.zone==us"},
	)
	if ok {
		t.Fatal("ok = true though the second constraint fails")
	}
	if reason != "node.labels.zone==us" {
		t.Errorf("reason = %q, want the failing constraint", reason)
	}
}

func TestNodeSatisfiesNoConstraints(t *testing.T) {
	if ok, _ := nodeSatisfies(gpuNode(), nil); !ok {
		t.Error("a service with no constraints must be placeable anywhere")
	}
}

// An unparseable or unknown constraint must not be silently read as satisfied
// — reporting a service movable when Swarm would refuse it is the one wrong
// answer this whole view exists to avoid. It is reported as not evaluable.
func TestNodeSatisfiesRefusesWhatItCannotEvaluate(t *testing.T) {
	for _, constraint := range []string{"node.labels.gpu", "weird.key==1", "node.role>=manager"} {
		ok, reason := nodeSatisfies(gpuNode(), []string{constraint})
		if ok {
			t.Errorf("%q read as satisfied; an unevaluable constraint must not be", constraint)
		}
		if reason == "" {
			t.Errorf("%q gave no reason", constraint)
		}
	}
}

// Docker accepts whitespace around the operator; a spec written by hand
// commonly carries it.
func TestNodeSatisfiesToleratesWhitespace(t *testing.T) {
	if ok, _ := nodeSatisfies(gpuNode(), []string{" node.labels.gpu == true "}); !ok {
		t.Error("ok = false for a constraint written with spaces")
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
// spec Docker 29 recorded for `nginx:alpine` against a node whose engine
// reports "aarch64" — the pairing that showed the naive comparison strands an
// ordinary multi-arch service on the very node it is already running on.
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

// A node that publishes one half of its platform and not the other must not be
// ruled out on the half it never stated. The guard originally required *both*
// halves to be absent before it stopped blocking, so a node reporting
// `OS: linux` with no architecture — a partial inspect, or an engine too old to
// publish one — failed every non-wildcard entry and stranded every service on
// it, which is the failure the check was hardened to avoid.
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
