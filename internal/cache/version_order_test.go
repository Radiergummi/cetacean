package cache

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// Two inspects of one resource can land in either order; the older must not
// overwrite the newer, nor announce itself as a change.
func TestSetIgnoresAnOlderVersion(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	versioned := func(index uint64) swarm.Meta {
		return swarm.Meta{Version: swarm.Version{Index: index}}
	}

	c.SetService(swarm.Service{ID: "s", Meta: versioned(5), Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "new"},
	}})
	c.SetTask(swarm.Task{ID: "t", Meta: versioned(5), Status: swarm.TaskStatus{
		State: swarm.TaskStateRunning,
	}})
	c.SetNode(swarm.Node{ID: "n", Meta: versioned(5), Spec: swarm.NodeSpec{
		Availability: swarm.NodeAvailabilityActive,
	}})
	before := len(events)

	c.SetService(swarm.Service{ID: "s", Meta: versioned(4), Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "old"},
	}})
	c.SetTask(swarm.Task{ID: "t", Meta: versioned(4), Status: swarm.TaskStatus{
		State: swarm.TaskStatePreparing,
	}})
	c.SetNode(swarm.Node{ID: "n", Meta: versioned(4), Spec: swarm.NodeSpec{
		Availability: swarm.NodeAvailabilityDrain,
	}})

	if s, _ := c.GetService("s"); s.Spec.Name != "new" {
		t.Errorf("service name = %q, want the newer write kept", s.Spec.Name)
	}
	if task, _ := c.GetTask("t"); task.Status.State != swarm.TaskStateRunning {
		t.Errorf("task state = %q, want the newer write kept", task.Status.State)
	}
	if n, _ := c.GetNode("n"); n.Spec.Availability != swarm.NodeAvailabilityActive {
		t.Errorf("node availability = %q, want the newer write kept", n.Spec.Availability)
	}
	if len(events) != before {
		t.Errorf("stale writes emitted %d events, want none", len(events)-before)
	}

	// An equal version is the same object read twice, and still applies.
	c.SetService(swarm.Service{ID: "s", Meta: versioned(5), Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "same"},
	}})
	if s, _ := c.GetService("s"); s.Spec.Name != "same" {
		t.Errorf("service name = %q, want an equal version applied", s.Spec.Name)
	}
}
