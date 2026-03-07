package cache

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func TestCache_SetGetNode(t *testing.T) {
	c := New(nil)
	node := swarm.Node{ID: "node1"}
	node.Description.Hostname = "host1"

	c.SetNode(node)

	got, ok := c.GetNode("node1")
	if !ok {
		t.Fatal("expected node to exist")
	}
	if got.Description.Hostname != "host1" {
		t.Errorf("expected host1, got %s", got.Description.Hostname)
	}
}

func TestCache_DeleteNode(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{ID: "node1"})
	c.DeleteNode("node1")

	_, ok := c.GetNode("node1")
	if ok {
		t.Fatal("expected node to be deleted")
	}
}

func TestCache_ListNodes(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetNode(swarm.Node{ID: "n2"})

	nodes := c.ListNodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestCache_SetService_DerivedStack(t *testing.T) {
	c := New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "mystack_web"
	svc.Spec.Labels = map[string]string{
		"com.docker.stack.namespace": "mystack",
	}

	c.SetService(svc)

	stack, ok := c.GetStack("mystack")
	if !ok {
		t.Fatal("expected stack to be derived")
	}
	if len(stack.Services) != 1 || stack.Services[0] != "svc1" {
		t.Errorf("expected stack to contain svc1, got %v", stack.Services)
	}
}

func TestCache_OnChange_Called(t *testing.T) {
	var called bool
	var gotEvent Event
	c := New(func(e Event) {
		called = true
		gotEvent = e
	})

	c.SetNode(swarm.Node{ID: "node1"})

	if !called {
		t.Fatal("expected onChange to be called")
	}
	if gotEvent.Type != "node" || gotEvent.Action != "update" || gotEvent.ID != "node1" {
		t.Errorf("unexpected event: %+v", gotEvent)
	}
}

func TestCache_ListTasksByService(t *testing.T) {
	c := New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1"})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc2"})
	c.SetTask(swarm.Task{ID: "t3", ServiceID: "svc1"})

	tasks := c.ListTasksByService("svc1")
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestCache_ListTasksByNode(t *testing.T) {
	c := New(nil)
	c.SetTask(swarm.Task{ID: "t1", NodeID: "node1"})
	c.SetTask(swarm.Task{ID: "t2", NodeID: "node2"})

	tasks := c.ListTasksByNode("node1")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "t1" {
		t.Errorf("expected t1, got %s", tasks[0].ID)
	}
}

func TestCache_Snapshot(t *testing.T) {
	c := New(nil)

	n1 := swarm.Node{ID: "n1"}
	n1.Status.State = swarm.NodeStateReady
	n2 := swarm.Node{ID: "n2"}
	n2.Status.State = swarm.NodeStateDown
	c.SetNode(n1)
	c.SetNode(n2)

	c.SetService(swarm.Service{ID: "s1"})

	t1 := swarm.Task{ID: "t1"}
	t1.Status.State = swarm.TaskStateRunning
	t2 := swarm.Task{ID: "t2"}
	t2.Status.State = swarm.TaskStateFailed
	c.SetTask(t1)
	c.SetTask(t2)

	snap := c.Snapshot()
	if snap.NodeCount != 2 {
		t.Errorf("expected 2 nodes, got %d", snap.NodeCount)
	}
	if snap.ServiceCount != 1 {
		t.Errorf("expected 1 service, got %d", snap.ServiceCount)
	}
	if snap.NodesReady != 1 {
		t.Errorf("expected 1 ready node, got %d", snap.NodesReady)
	}
	if snap.NodesDown != 1 {
		t.Errorf("expected 1 down node, got %d", snap.NodesDown)
	}
	if snap.TasksByState["running"] != 1 {
		t.Errorf("expected 1 running task, got %d", snap.TasksByState["running"])
	}
	if snap.TasksByState["failed"] != 1 {
		t.Errorf("expected 1 failed task, got %d", snap.TasksByState["failed"])
	}
}
