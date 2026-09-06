package cluster

import (
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
