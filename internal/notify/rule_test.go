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
		Match:   Match{Condition: "state == failed"},
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
