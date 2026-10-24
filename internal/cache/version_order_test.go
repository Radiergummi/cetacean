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

// A full sync's lists can be read before an event's inspect lands; the older
// listed copy must not put back what the inspect replaced, though anything the
// lists lack is still removed.
func TestReplaceAllKeepsTheNewerOfCachedAndListed(t *testing.T) {
	c := New(nil)

	versioned := func(index uint64) swarm.Meta {
		return swarm.Meta{Version: swarm.Version{Index: index}}
	}

	c.SetService(swarm.Service{ID: "s", Meta: versioned(5), Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "new"},
	}})
	c.SetService(swarm.Service{ID: "gone", Meta: versioned(1)})
	c.SetTask(swarm.Task{ID: "t", Meta: versioned(5), NodeID: "n", ServiceID: "s",
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetNode(swarm.Node{ID: "n", Meta: versioned(5), Spec: swarm.NodeSpec{
		Availability: swarm.NodeAvailabilityActive,
	}})
	c.SetConfig(swarm.Config{ID: "c", Meta: versioned(5), Spec: swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: "new"},
	}})
	c.SetSecret(swarm.Secret{ID: "x", Meta: versioned(5), Spec: swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: "new"},
	}})

	c.ReplaceAll(FullSyncData{
		Services: []swarm.Service{{ID: "s", Meta: versioned(4), Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "old"},
		}}},
		Tasks: []swarm.Task{{ID: "t", Meta: versioned(4), ServiceID: "s",
			Status: swarm.TaskStatus{State: swarm.TaskStatePending}}},
		Nodes: []swarm.Node{{ID: "n", Meta: versioned(4), Spec: swarm.NodeSpec{
			Availability: swarm.NodeAvailabilityDrain,
		}}},
		Configs: []swarm.Config{{ID: "c", Meta: versioned(4), Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: "old"},
		}}},
		Secrets: []swarm.Secret{{ID: "x", Meta: versioned(4), Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "old"},
		}}},
		HasNodes: true, HasServices: true, HasTasks: true, HasConfigs: true, HasSecrets: true,
	})

	if s, _ := c.GetService("s"); s.Spec.Name != "new" {
		t.Errorf("service name = %q, want the newer cached copy kept", s.Spec.Name)
	}
	if _, ok := c.GetService("gone"); ok {
		t.Error("a service the lists lack survived the sync")
	}
	if task, _ := c.GetTask("t"); task.Status.State != swarm.TaskStateRunning {
		t.Errorf("task state = %q, want the newer cached copy kept", task.Status.State)
	}
	if tasks := c.ListTasksByNode("n"); len(tasks) != 1 {
		t.Errorf("tasks on node = %d, want the kept task indexed by its node", len(tasks))
	}
	if n, _ := c.GetNode("n"); n.Spec.Availability != swarm.NodeAvailabilityActive {
		t.Errorf("node availability = %q, want the newer cached copy kept", n.Spec.Availability)
	}
	if cfg, _ := c.GetConfig("c"); cfg.Spec.Name != "new" {
		t.Errorf("config name = %q, want the newer cached copy kept", cfg.Spec.Name)
	}
	if s, _ := c.GetSecret("x"); s.Spec.Name != "new" {
		t.Errorf("secret name = %q, want the newer cached copy kept", s.Spec.Name)
	}

	// A newer listed copy still replaces the cached one.
	c.ReplaceAll(FullSyncData{
		Services: []swarm.Service{{ID: "s", Meta: versioned(6), Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "newest"},
		}}},
		HasServices: true,
	})
	if s, _ := c.GetService("s"); s.Spec.Name != "newest" {
		t.Errorf("service name = %q, want the newer listed copy applied", s.Spec.Name)
	}
}
