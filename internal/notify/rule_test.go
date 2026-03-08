package notify

import (
	"testing"

	"cetacean/internal/cache"

	"github.com/docker/docker/api/types/swarm"
)

func TestRule_Matches_TypeAndAction(t *testing.T) {
	r := Rule{
		Enabled: true,
		Match:   Match{Type: "task", Action: "update"},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	evt := cache.Event{Type: "task", Action: "update", Resource: swarm.Task{}}
	if !r.matches(evt, "web.1") {
		t.Error("expected match on type+action")
	}

	evt.Type = "node"
	if r.matches(evt, "web.1") {
		t.Error("expected no match on wrong type")
	}
}

func TestRule_Matches_NameRegex(t *testing.T) {
	r := Rule{
		Enabled: true,
		Match:   Match{NameRegex: `^web\.`},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	evt := cache.Event{Type: "task", Action: "update", Resource: swarm.Task{}}
	if !r.matches(evt, "web.1") {
		t.Error("expected match on name regex")
	}
	if r.matches(evt, "api.1") {
		t.Error("expected no match on non-matching name")
	}
}

func TestRule_Matches_Condition(t *testing.T) {
	r := Rule{
		Enabled: true,
		Match:   Match{Condition: `state == "failed"`},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	failed := cache.Event{
		Type:     "task",
		Action:   "update",
		Resource: swarm.Task{Status: swarm.TaskStatus{State: swarm.TaskStateFailed}},
	}
	if !r.matches(failed, "web.1") {
		t.Error("expected match on state == failed")
	}

	running := cache.Event{
		Type:     "task",
		Action:   "update",
		Resource: swarm.Task{Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
	}
	if r.matches(running, "web.1") {
		t.Error("expected no match on state == running")
	}
}

func TestRule_Matches_ConditionComplex(t *testing.T) {
	r := Rule{
		Enabled: true,
		Match:   Match{Condition: `state == "failed" && exit_code != "0"`},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	evt := cache.Event{
		Type:   "task",
		Action: "update",
		Resource: swarm.Task{
			Status: swarm.TaskStatus{
				State:           swarm.TaskStateFailed,
				ContainerStatus: &swarm.ContainerStatus{ExitCode: 1},
			},
		},
	}
	if !r.matches(evt, "web.1") {
		t.Error("expected match on complex condition")
	}
}

func TestRule_Matches_ConditionContains(t *testing.T) {
	r := Rule{
		Enabled: true,
		Match:   Match{Condition: `image contains "nginx"`},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	evt := cache.Event{
		Type:   "service",
		Action: "update",
		Resource: swarm.Service{
			Spec: swarm.ServiceSpec{
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.25"},
				},
			},
		},
	}
	if !r.matches(evt, "web") {
		t.Error("expected match on image contains nginx")
	}
}

func TestRule_Matches_ConditionNilResource(t *testing.T) {
	r := Rule{
		Enabled: true,
		Match:   Match{Condition: `state == "failed"`},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	// Remove events have nil Resource
	evt := cache.Event{Type: "task", Action: "remove", Resource: nil}
	if r.matches(evt, "web.1") {
		t.Error("nil resource should not match condition")
	}
}

func TestRule_Matches_NodeCondition(t *testing.T) {
	r := Rule{
		Enabled: true,
		Match:   Match{Condition: `role == "manager" && state == "ready"`},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	evt := cache.Event{
		Type:   "node",
		Action: "update",
		Resource: swarm.Node{
			Spec:   swarm.NodeSpec{Role: swarm.NodeRoleManager},
			Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		},
	}
	if !r.matches(evt, "node-1") {
		t.Error("expected match on node role+state")
	}
}

func TestRule_Matches_Disabled(t *testing.T) {
	r := Rule{
		Enabled: false,
		Match:   Match{Type: "task"},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	evt := cache.Event{Type: "task", Action: "update", Resource: swarm.Task{}}
	if r.matches(evt, "web.1") {
		t.Error("disabled rule should never match")
	}
}

func TestRule_Compile_BadRegex(t *testing.T) {
	r := Rule{
		Enabled: true,
		Match:   Match{NameRegex: `[invalid`},
	}
	if err := r.compile(); err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestRule_Compile_BadCondition(t *testing.T) {
	r := Rule{
		Enabled: true,
		Match:   Match{Condition: `state ===`},
	}
	if err := r.compile(); err == nil {
		t.Error("expected error for invalid condition expression")
	}
}

func TestRule_Matches_ConditionOrLogic(t *testing.T) {
	r := Rule{
		Enabled: true,
		Match:   Match{Condition: `state == "failed" || state == "rejected"`},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	rejected := cache.Event{
		Type:     "task",
		Action:   "update",
		Resource: swarm.Task{Status: swarm.TaskStatus{State: swarm.TaskStateRejected}},
	}
	if !r.matches(rejected, "web.1") {
		t.Error("expected match on OR condition")
	}

	running := cache.Event{
		Type:     "task",
		Action:   "update",
		Resource: swarm.Task{Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
	}
	if r.matches(running, "web.1") {
		t.Error("expected no match for running on OR condition")
	}
}

func TestRule_Matches_ConditionWrongResourceType(t *testing.T) {
	// "role" exists on nodes but not tasks — should not match
	r := Rule{
		Enabled: true,
		Match:   Match{Condition: `role == "manager"`},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	evt := cache.Event{
		Type:     "task",
		Action:   "update",
		Resource: swarm.Task{Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
	}
	if r.matches(evt, "web.1") {
		t.Error("task has no role field, should not match")
	}
}

func TestRule_Matches_NoCondition(t *testing.T) {
	// Empty condition should always pass (match on type/action only)
	r := Rule{
		Enabled: true,
		Match:   Match{Type: "task", Action: "update"},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	evt := cache.Event{Type: "task", Action: "update", Resource: swarm.Task{}}
	if !r.matches(evt, "web.1") {
		t.Error("empty condition should match")
	}
}
