package filter

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// An interned value has to be indistinguishable from the string it replaces:
// the expression engine compares against string literals, and an env holding
// anything else silently stops matching.
func TestInternedValuesAreStrings(t *testing.T) {
	env := NodeEnv(swarm.Node{
		ID: "n1",
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityDrain,
		},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		Description: swarm.NodeDescription{Hostname: "host"},
	}, nil)

	for field, want := range map[string]string{
		"state":        "ready",
		"role":         "manager",
		"availability": "drain",
	} {
		got, ok := env[field].(string)
		if !ok {
			t.Errorf("%s is %T, not a string", field, env[field])
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
}

// A state this build does not know still has to arrive as its own text.
func TestUninternedValueStillBoxed(t *testing.T) {
	env := NodeEnv(swarm.Node{
		Status: swarm.NodeStatus{State: swarm.NodeState("some-future-state")},
	}, nil)

	if got := env["state"]; got != "some-future-state" {
		t.Errorf("state = %#v, want the raw value boxed", got)
	}
}

// Both branches of the mode field, and an absent container spec.
func TestServiceEnvModes(t *testing.T) {
	global := ServiceEnv(swarm.Service{
		Spec: swarm.ServiceSpec{Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}}},
	}, nil)
	if got := global["mode"]; got != "global" {
		t.Errorf("global service mode = %#v", got)
	}
	if got := global["image"]; got != "" {
		t.Errorf("a service with no container spec reported image %#v", got)
	}

	replicated := ServiceEnv(swarm.Service{}, nil)
	if got := replicated["mode"]; got != "replicated" {
		t.Errorf("replicated service mode = %#v", got)
	}
}

// Filtering has to keep working end to end, since the env is what it reads.
func TestFilterMatchesInternedFields(t *testing.T) {
	prog, err := Compile(`state == "ready" && role == "manager"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	node := swarm.Node{
		Spec:   swarm.NodeSpec{Role: swarm.NodeRoleManager},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
	}
	ok, err := Evaluate(prog, NodeEnv(node, nil))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !ok {
		t.Error("a ready manager did not match its own filter")
	}
}
