package cluster_test

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// Resolution took the first match in (Slot, ID) order, so a service that had
// been updated resolved to whichever record's ID sorted first.
func TestResolveTaskPrefersTheLiveTaskInASlot(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetTask(swarm.Task{
		ID: "aaa-dead", ServiceID: "svc1", Slot: 1,
		DesiredState: swarm.TaskStateShutdown,
		Status:       swarm.TaskStatus{State: swarm.TaskStateFailed},
	})
	c.SetTask(swarm.Task{
		ID: "zzz-live", ServiceID: "svc1", Slot: 1,
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	task, ok, err := cluster.ResolveTask(c, "web.1")
	if err != nil || !ok {
		t.Fatalf("ResolveTask(web.1) = (_, %v, %v), want a task", ok, err)
	}

	if task.ID != "zzz-live" {
		t.Errorf("ResolveTask(web.1) = %q, want %q", task.ID, "zzz-live")
	}
}

// The most recent record in an empty slot answers a different question than
// the one asked.
func TestResolveTaskFindsNothingInASlotHoldingOnlyHistory(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetTask(swarm.Task{
		ID: "old", ServiceID: "svc1", Slot: 1,
		DesiredState: swarm.TaskStateShutdown,
		Status:       swarm.TaskStatus{State: swarm.TaskStateFailed},
	})
	c.SetTask(swarm.Task{
		ID: "older", ServiceID: "svc1", Slot: 1,
		DesiredState: swarm.TaskStateRemove,
		Status:       swarm.TaskStatus{State: swarm.TaskStateShutdown},
	})

	task, ok, err := cluster.ResolveTask(c, "web.1")
	if err != nil {
		t.Fatalf("ResolveTask(web.1): %v", err)
	}

	if ok {
		t.Errorf("ResolveTask(web.1) = %q, want no task", task.ID)
	}
}

// The live preference is for the name form alone; reading history by ID is
// what the task detail view and get_logs do.
func TestResolveTaskReturnsATerminalTaskByID(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetTask(swarm.Task{
		ID: "aaa-dead", ServiceID: "svc1", Slot: 1,
		DesiredState: swarm.TaskStateShutdown,
	})

	task, ok, err := cluster.ResolveTask(c, "aaa-dead")
	if err != nil || !ok {
		t.Fatalf("ResolveTask(aaa-dead) = (_, %v, %v), want a task", ok, err)
	}

	if task.ID != "aaa-dead" {
		t.Errorf("ResolveTask(aaa-dead) = %q, want %q", task.ID, "aaa-dead")
	}
}

func TestResolveTaskResolvesAGlobalTaskByNode(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID: "svc-agent",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "agent"},
			Mode:        swarm.ServiceMode{Global: &swarm.GlobalService{}},
		},
	})
	c.SetTask(swarm.Task{
		ID: "task-node2", ServiceID: "svc-agent", NodeID: "node-2",
		DesiredState: swarm.TaskStateRunning,
	})

	task, ok, err := cluster.ResolveTask(c, "agent.node-2")
	if err != nil || !ok {
		t.Fatalf("ResolveTask(agent.node-2) = (_, %v, %v), want a task", ok, err)
	}

	if task.ID != "task-node2" {
		t.Errorf("ResolveTask(agent.node-2) = %q, want %q", task.ID, "task-node2")
	}
}

// With no node to render, TaskName gives it the bare service name — so an
// identifier with no separator at all is not a miss.
func TestResolveTaskResolvesAnUnassignedGlobalTask(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID: "svc-agent",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "agent"},
			Mode:        swarm.ServiceMode{Global: &swarm.GlobalService{}},
		},
	})
	c.SetTask(swarm.Task{
		ID: "task-pending", ServiceID: "svc-agent",
		DesiredState: swarm.TaskStateRunning,
	})

	task, ok, err := cluster.ResolveTask(c, "agent")
	if err != nil || !ok {
		t.Fatalf("ResolveTask(agent) = (_, %v, %v), want a task", ok, err)
	}

	if task.ID != "task-pending" {
		t.Errorf("ResolveTask(agent) = %q, want %q", task.ID, "task-pending")
	}
}

// Docker permits a dot in a service name; neither half of the suffix TaskName
// appends can hold one.
func TestResolveTaskSplitsAServiceNameHoldingDots(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID:   "svc-api",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api.example.com"}},
	})
	c.SetTask(swarm.Task{
		ID: "task-xyz", ServiceID: "svc-api", Slot: 2,
		DesiredState: swarm.TaskStateRunning,
	})

	task, ok, err := cluster.ResolveTask(c, "api.example.com.2")
	if err != nil || !ok {
		t.Fatalf("ResolveTask(api.example.com.2) = (_, %v, %v), want a task", ok, err)
	}

	if task.ID != "task-xyz" {
		t.Errorf("ResolveTask(api.example.com.2) = %q, want %q", task.ID, "task-xyz")
	}
}

// An ambiguous service cannot become a task miss — the caller is told which
// service they meant, not that their task is absent.
func TestResolveTaskReportsAnAmbiguousServiceName(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID:   "svc-a",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc-b",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	if _, _, err := cluster.ResolveTask(c, "web.1"); err == nil {
		t.Error("ResolveTask(web.1): want an ambiguity error, got nil")
	}
}

func TestResolveTaskUnknownNameIsNotFound(t *testing.T) {
	c := newTestCache()

	task, ok, err := cluster.ResolveTask(c, "nope.1")
	if err != nil {
		t.Fatalf("ResolveTask(nope.1): %v", err)
	}

	if ok {
		t.Errorf("ResolveTask(nope.1) = %q, want no task", task.ID)
	}
}

// Updating a global service leaves the replaced record on the node now holding
// its successor, under the same name — so the ambiguity is not slots alone.
func TestResolveTaskPrefersTheLiveTaskOnANode(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID: "svc-agent",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "agent"},
			Mode:        swarm.ServiceMode{Global: &swarm.GlobalService{}},
		},
	})
	c.SetTask(swarm.Task{
		ID: "aaa-dead", ServiceID: "svc-agent", NodeID: "node-2",
		DesiredState: swarm.TaskStateShutdown,
		Status:       swarm.TaskStatus{State: swarm.TaskStateFailed},
	})
	c.SetTask(swarm.Task{
		ID: "zzz-live", ServiceID: "svc-agent", NodeID: "node-2",
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	task, ok, err := cluster.ResolveTask(c, "agent.node-2")
	if err != nil || !ok {
		t.Fatalf("ResolveTask(agent.node-2) = (_, %v, %v), want a task", ok, err)
	}

	if task.ID != "zzz-live" {
		t.Errorf("ResolveTask(agent.node-2) = %q, want %q", task.ID, "zzz-live")
	}
}

// A name-addressed history query is the reason this exists: the identifier
// travels in a query parameter, which the canonical redirect never rewrites.
func TestResolveIdentifierTurnsANameIntoTheIDTheRingKeysBy(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "shop_web"}},
	})

	id, err := cluster.ResolveIdentifier(c, "service", "shop_web")
	if err != nil {
		t.Fatalf("ResolveIdentifier(service, shop_web) = %v", err)
	}

	if id != "svc1" {
		t.Errorf("ResolveIdentifier(service, shop_web) = %q, want %q", id, "svc1")
	}
}

// The ring outlives the cache's knowledge of a resource, so an entry for one
// that has since been removed has to stay reachable by the ID it was stored
// under.
func TestResolveIdentifierPassesAnUnmatchedIdentifierThrough(t *testing.T) {
	c := newTestCache()

	for _, resourceType := range []string{"service", "volume", "nonsense", ""} {
		id, err := cluster.ResolveIdentifier(c, resourceType, "gone")
		if err != nil {
			t.Fatalf("ResolveIdentifier(%q, gone) = %v", resourceType, err)
		}

		if id != "gone" {
			t.Errorf("ResolveIdentifier(%q, gone) = %q, want %q", resourceType, id, "gone")
		}
	}
}

// Volumes and stacks are keyed by name, so there is nothing to resolve and the
// identifier must survive a type that has no resolver.
func TestResolveIdentifierLeavesANameKeyedTypeAlone(t *testing.T) {
	c := newTestCache()

	id, err := cluster.ResolveIdentifier(c, "volume", "pgdata")
	if err != nil {
		t.Fatalf("ResolveIdentifier(volume, pgdata) = %v", err)
	}

	if id != "pgdata" {
		t.Errorf("ResolveIdentifier(volume, pgdata) = %q, want %q", id, "pgdata")
	}
}

// Without a type the search spans every type, which is worth doing only while
// the answer is unambiguous.
func TestResolveIdentifierWithoutATypeResolvesOnlyAUniqueMatch(t *testing.T) {
	c := newTestCache()
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "unique"}},
	})
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "shared"}},
	})
	c.SetSecret(swarm.Secret{
		ID:   "sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "shared"}},
	})

	id, err := cluster.ResolveIdentifier(c, "", "unique")
	if err != nil {
		t.Fatalf("ResolveIdentifier(, unique) = %v", err)
	}

	if id != "svc1" {
		t.Errorf("ResolveIdentifier(, unique) = %q, want %q", id, "svc1")
	}

	// A config and a secret both named "shared" answer different questions,
	// and picking either would be picking whichever map ranged first.
	id, err = cluster.ResolveIdentifier(c, "", "shared")
	if err != nil {
		t.Fatalf("ResolveIdentifier(, shared) = %v", err)
	}

	if id != "shared" {
		t.Errorf("ResolveIdentifier(, shared) = %q, want it left alone", id)
	}

	// Naming the type is what makes it answerable.
	id, err = cluster.ResolveIdentifier(c, "secret", "shared")
	if err != nil {
		t.Fatalf("ResolveIdentifier(secret, shared) = %v", err)
	}

	if id != "sec1" {
		t.Errorf("ResolveIdentifier(secret, shared) = %q, want %q", id, "sec1")
	}
}
