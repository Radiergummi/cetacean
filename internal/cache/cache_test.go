package cache

import (
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
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

func TestCache_RunningTaskCount(t *testing.T) {
	c := New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateFailed}})
	c.SetTask(swarm.Task{ID: "t3", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t4", ServiceID: "svc2", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})

	if got := c.RunningTaskCount("svc1"); got != 2 {
		t.Errorf("RunningTaskCount(svc1) = %d, want 2", got)
	}
	if got := c.RunningTaskCount("svc2"); got != 1 {
		t.Errorf("RunningTaskCount(svc2) = %d, want 1", got)
	}
	if got := c.RunningTaskCount("missing"); got != 0 {
		t.Errorf("RunningTaskCount(missing) = %d, want 0", got)
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

func TestCache_ReplaceNodes_RemovesStale(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetNode(swarm.Node{ID: "n2"})
	c.SetNode(swarm.Node{ID: "n3"})

	// Replace with only n1 and n4 — n2 and n3 should be pruned
	c.replaceNodes([]swarm.Node{{ID: "n1"}, {ID: "n4"}})

	nodes := c.ListNodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if _, ok := c.GetNode("n2"); ok {
		t.Error("expected n2 to be pruned")
	}
	if _, ok := c.GetNode("n3"); ok {
		t.Error("expected n3 to be pruned")
	}
	if _, ok := c.GetNode("n4"); !ok {
		t.Error("expected n4 to exist")
	}
}

// --- Delete operations for all resource types ---

func TestCache_DeleteService(t *testing.T) {
	c := New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Labels = map[string]string{stackLabel: "mystack"}
	c.SetService(svc)

	c.DeleteService("svc1")

	if _, ok := c.GetService("svc1"); ok {
		t.Fatal("expected service to be deleted")
	}
	// Stack should also be removed since it was the only resource
	if _, ok := c.GetStack("mystack"); ok {
		t.Fatal("expected stack to be removed when last resource deleted")
	}
}

func TestCache_DeleteTask(t *testing.T) {
	c := New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", NodeID: "node1"})

	c.DeleteTask("t1")

	if _, ok := c.GetTask("t1"); ok {
		t.Fatal("expected task to be deleted")
	}
	if tasks := c.ListTasksByService("svc1"); len(tasks) != 0 {
		t.Errorf("expected service index cleaned up, got %d tasks", len(tasks))
	}
	if tasks := c.ListTasksByNode("node1"); len(tasks) != 0 {
		t.Errorf("expected node index cleaned up, got %d tasks", len(tasks))
	}
}

func TestCache_DeleteConfig(t *testing.T) {
	c := New(nil)
	cfg := swarm.Config{ID: "cfg1"}
	cfg.Spec.Labels = map[string]string{stackLabel: "mystack"}
	c.SetConfig(cfg)

	c.DeleteConfig("cfg1")

	if _, ok := c.GetConfig("cfg1"); ok {
		t.Fatal("expected config to be deleted")
	}
}

func TestCache_DeleteSecret(t *testing.T) {
	c := New(nil)
	sec := swarm.Secret{ID: "sec1"}
	sec.Spec.Labels = map[string]string{stackLabel: "mystack"}
	c.SetSecret(sec)

	c.DeleteSecret("sec1")

	if _, ok := c.GetSecret("sec1"); ok {
		t.Fatal("expected secret to be deleted")
	}
}

func TestCache_DeleteNetwork(t *testing.T) {
	c := New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Labels: map[string]string{stackLabel: "mystack"}})

	c.DeleteNetwork("net1")

	if _, ok := c.GetNetwork("net1"); ok {
		t.Fatal("expected network to be deleted")
	}
}

func TestCache_DeleteVolume(t *testing.T) {
	c := New(nil)
	c.SetVolume(volume.Volume{Name: "vol1", Labels: map[string]string{stackLabel: "mystack"}})

	c.DeleteVolume("vol1")

	if _, ok := c.GetVolume("vol1"); ok {
		t.Fatal("expected volume to be deleted")
	}
}

func TestCache_DeleteNonexistent(t *testing.T) {
	c := New(nil)
	// Should not panic
	c.DeleteNode("nope")
	c.DeleteService("nope")
	c.DeleteTask("nope")
	c.DeleteConfig("nope")
	c.DeleteSecret("nope")
	c.DeleteNetwork("nope")
	c.DeleteVolume("nope")
}

// --- Stack lifecycle ---

func TestCache_Stack_MultipleResourceTypes(t *testing.T) {
	c := New(nil)
	labels := map[string]string{stackLabel: "mystack"}

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Labels = labels
	c.SetService(svc)

	cfg := swarm.Config{ID: "cfg1"}
	cfg.Spec.Labels = labels
	c.SetConfig(cfg)

	sec := swarm.Secret{ID: "sec1"}
	sec.Spec.Labels = labels
	c.SetSecret(sec)

	c.SetNetwork(network.Summary{ID: "net1", Labels: labels})
	c.SetVolume(volume.Volume{Name: "vol1", Labels: labels})

	stack, ok := c.GetStack("mystack")
	if !ok {
		t.Fatal("expected stack to exist")
	}
	if len(stack.Services) != 1 || len(stack.Configs) != 1 || len(stack.Secrets) != 1 || len(stack.Networks) != 1 || len(stack.Volumes) != 1 {
		t.Errorf("expected 1 of each resource, got services=%d configs=%d secrets=%d networks=%d volumes=%d",
			len(stack.Services), len(stack.Configs), len(stack.Secrets), len(stack.Networks), len(stack.Volumes))
	}
}

func TestCache_Stack_PartialRemoval(t *testing.T) {
	c := New(nil)
	labels := map[string]string{stackLabel: "mystack"}

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Labels = labels
	c.SetService(svc)

	cfg := swarm.Config{ID: "cfg1"}
	cfg.Spec.Labels = labels
	c.SetConfig(cfg)

	// Remove service — stack should be gone (stacks require at least one service)
	c.DeleteService("svc1")

	if _, ok := c.GetStack("mystack"); ok {
		t.Fatal("expected stack to be removed when last service is deleted")
	}
}

func TestCache_Stack_NoGhostAfterServiceDeletion(t *testing.T) {
	c := New(nil)
	labels := map[string]string{stackLabel: "mystack"}

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Labels = labels
	c.SetService(svc)

	cfg := swarm.Config{ID: "cfg1"}
	cfg.Spec.Labels = labels
	c.SetConfig(cfg)

	// Delete the only service — stack should be removed
	c.DeleteService("svc1")

	// Re-setting the config (e.g. from a Docker event) must NOT recreate the stack
	c.SetConfig(cfg)

	if _, ok := c.GetStack("mystack"); ok {
		t.Fatal("expected no ghost stack after service deletion and config re-set")
	}
	if stacks := c.ListStacks(); len(stacks) != 0 {
		t.Errorf("expected 0 stacks, got %d", len(stacks))
	}
}

func TestCache_Stack_NoLabelNoStack(t *testing.T) {
	c := New(nil)
	// Service without stack label should not create a stack
	c.SetService(swarm.Service{ID: "svc1"})

	if stacks := c.ListStacks(); len(stacks) != 0 {
		t.Errorf("expected 0 stacks, got %d", len(stacks))
	}
}

func TestCache_Stack_ServiceUpdateChangesStack(t *testing.T) {
	c := New(nil)

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Labels = map[string]string{stackLabel: "stackA"}
	c.SetService(svc)

	// Update same service to belong to a different stack
	svc.Spec.Labels = map[string]string{stackLabel: "stackB"}
	c.SetService(svc)

	if _, ok := c.GetStack("stackA"); ok {
		t.Fatal("expected stackA to be removed")
	}
	stack, ok := c.GetStack("stackB")
	if !ok {
		t.Fatal("expected stackB to exist")
	}
	if len(stack.Services) != 1 {
		t.Errorf("expected 1 service in stackB, got %d", len(stack.Services))
	}
}

func TestCache_Stack_DuplicateAdd(t *testing.T) {
	c := New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Labels = map[string]string{stackLabel: "mystack"}

	c.SetService(svc)
	c.SetService(svc) // same service again

	stack, _ := c.GetStack("mystack")
	if len(stack.Services) != 1 {
		t.Errorf("expected 1 service (no duplicates), got %d", len(stack.Services))
	}
}

// --- Task index management ---

func TestCache_TaskIndex_Reassignment(t *testing.T) {
	c := New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", NodeID: "node1"})

	// Reassign task to different service and node
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc2", NodeID: "node2"})

	if tasks := c.ListTasksByService("svc1"); len(tasks) != 0 {
		t.Errorf("expected 0 tasks for svc1, got %d", len(tasks))
	}
	if tasks := c.ListTasksByNode("node1"); len(tasks) != 0 {
		t.Errorf("expected 0 tasks for node1, got %d", len(tasks))
	}
	if tasks := c.ListTasksByService("svc2"); len(tasks) != 1 {
		t.Errorf("expected 1 task for svc2, got %d", len(tasks))
	}
	if tasks := c.ListTasksByNode("node2"); len(tasks) != 1 {
		t.Errorf("expected 1 task for node2, got %d", len(tasks))
	}
}

func TestCache_TaskIndex_EmptyServiceAndNode(t *testing.T) {
	c := New(nil)
	// Task with no service or node should not panic
	c.SetTask(swarm.Task{ID: "t1"})
	c.DeleteTask("t1")
}

// --- GetStackDetail ---

func TestCache_GetStackDetail(t *testing.T) {
	c := New(nil)
	labels := map[string]string{stackLabel: "mystack"}

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "mystack_web"
	svc.Spec.Labels = labels
	c.SetService(svc)

	cfg := swarm.Config{ID: "cfg1"}
	cfg.Spec.Name = "mystack_config"
	cfg.Spec.Labels = labels
	c.SetConfig(cfg)

	sec := swarm.Secret{ID: "sec1"}
	sec.Spec.Name = "mystack_secret"
	sec.Spec.Labels = labels
	c.SetSecret(sec)

	c.SetNetwork(network.Summary{ID: "net1", Name: "mystack_net", Labels: labels})
	c.SetVolume(volume.Volume{Name: "vol1", Labels: labels})

	detail, ok := c.GetStackDetail("mystack")
	if !ok {
		t.Fatal("expected stack detail to exist")
	}
	if detail.Name != "mystack" {
		t.Errorf("expected name mystack, got %s", detail.Name)
	}
	if len(detail.Services) != 1 || detail.Services[0].ID != "svc1" {
		t.Errorf("unexpected services: %v", detail.Services)
	}
	if len(detail.Configs) != 1 || detail.Configs[0].ID != "cfg1" {
		t.Errorf("unexpected configs: %v", detail.Configs)
	}
	if len(detail.Secrets) != 1 || detail.Secrets[0].ID != "sec1" {
		t.Errorf("unexpected secrets: %v", detail.Secrets)
	}
	if len(detail.Networks) != 1 || detail.Networks[0].ID != "net1" {
		t.Errorf("unexpected networks: %v", detail.Networks)
	}
	if len(detail.Volumes) != 1 || detail.Volumes[0].Name != "vol1" {
		t.Errorf("unexpected volumes: %v", detail.Volumes)
	}
}

func TestCache_GetStackDetail_NotFound(t *testing.T) {
	c := New(nil)
	_, ok := c.GetStackDetail("nonexistent")
	if ok {
		t.Fatal("expected stack detail not found")
	}
}

// --- Replace operations (bulk sync) ---

func TestCache_ReplaceServices_RebuildStacks(t *testing.T) {
	c := New(nil)

	// Set up initial state with a stack
	svc1 := swarm.Service{ID: "svc1"}
	svc1.Spec.Labels = map[string]string{stackLabel: "oldstack"}
	c.SetService(svc1)

	// Replace with different services
	svc2 := swarm.Service{ID: "svc2"}
	svc2.Spec.Labels = map[string]string{stackLabel: "newstack"}
	c.replaceServices([]swarm.Service{svc2})
	c.rebuildStacksSynced()

	if _, ok := c.GetService("svc1"); ok {
		t.Error("expected svc1 to be replaced")
	}
	if _, ok := c.GetService("svc2"); !ok {
		t.Error("expected svc2 to exist")
	}
	if _, ok := c.GetStack("oldstack"); ok {
		t.Error("expected oldstack to be gone after rebuild")
	}
	if _, ok := c.GetStack("newstack"); !ok {
		t.Error("expected newstack to exist after rebuild")
	}
}

func TestCache_ReplaceTasks_RebuildsIndexes(t *testing.T) {
	c := New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", NodeID: "node1"})

	c.replaceTasks([]swarm.Task{
		{ID: "t2", ServiceID: "svc2", NodeID: "node2"},
		{ID: "t3", ServiceID: "svc2", NodeID: "node2"},
	})

	if tasks := c.ListTasksByService("svc1"); len(tasks) != 0 {
		t.Errorf("expected old index cleared, got %d tasks for svc1", len(tasks))
	}
	if tasks := c.ListTasksByNode("node1"); len(tasks) != 0 {
		t.Errorf("expected old index cleared, got %d tasks for node1", len(tasks))
	}
	if tasks := c.ListTasksByService("svc2"); len(tasks) != 2 {
		t.Errorf("expected 2 tasks for svc2, got %d", len(tasks))
	}
	if tasks := c.ListTasksByNode("node2"); len(tasks) != 2 {
		t.Errorf("expected 2 tasks for node2, got %d", len(tasks))
	}
}

func TestCache_ReplaceConfigs_RebuildStacks(t *testing.T) {
	c := New(nil)
	cfg := swarm.Config{ID: "cfg1"}
	cfg.Spec.Labels = map[string]string{stackLabel: "mystack"}
	c.SetConfig(cfg)

	// Replace with empty — stack from configs should be gone
	c.replaceConfigs(nil)
	c.rebuildStacksSynced()

	if _, ok := c.GetConfig("cfg1"); ok {
		t.Error("expected cfg1 to be replaced")
	}
}

func TestCache_ReplaceSecrets_RebuildStacks(t *testing.T) {
	c := New(nil)
	sec := swarm.Secret{ID: "sec1"}
	sec.Spec.Labels = map[string]string{stackLabel: "mystack"}
	c.SetSecret(sec)

	sec2 := swarm.Secret{ID: "sec2"}
	sec2.Spec.Labels = map[string]string{stackLabel: "otherstack"}
	c.replaceSecrets([]swarm.Secret{sec2})
	c.rebuildStacksSynced()

	if _, ok := c.GetSecret("sec1"); ok {
		t.Error("expected sec1 to be replaced")
	}
	if _, ok := c.GetSecret("sec2"); !ok {
		t.Error("expected sec2 to exist")
	}
}

func TestCache_ReplaceNetworks_RebuildStacks(t *testing.T) {
	c := New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Labels: map[string]string{stackLabel: "mystack"}})

	c.replaceNetworks([]network.Summary{
		{ID: "net2", Labels: map[string]string{stackLabel: "newstack"}},
	})
	c.rebuildStacksSynced()

	if _, ok := c.GetNetwork("net1"); ok {
		t.Error("expected net1 to be replaced")
	}
	if _, ok := c.GetNetwork("net2"); !ok {
		t.Error("expected net2 to exist")
	}
}

func TestCache_ReplaceVolumes_RebuildStacks(t *testing.T) {
	c := New(nil)
	c.SetVolume(volume.Volume{Name: "vol1", Labels: map[string]string{stackLabel: "mystack"}})

	c.replaceVolumes([]volume.Volume{
		{Name: "vol2", Labels: map[string]string{stackLabel: "newstack"}},
	})
	c.rebuildStacksSynced()

	if _, ok := c.GetVolume("vol1"); ok {
		t.Error("expected vol1 to be replaced")
	}
	if _, ok := c.GetVolume("vol2"); !ok {
		t.Error("expected vol2 to exist")
	}
}

func TestCache_ReplaceStacks_CrossResourceRebuild(t *testing.T) {
	c := New(nil)
	labels := map[string]string{stackLabel: "mystack"}

	// Add service and config to same stack
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Labels = labels
	c.SetService(svc)

	cfg := swarm.Config{ID: "cfg1"}
	cfg.Spec.Labels = labels
	c.SetConfig(cfg)

	// Replace services with empty — stack should be gone (requires services)
	c.replaceServices(nil)
	c.rebuildStacksSynced()

	if _, ok := c.GetStack("mystack"); ok {
		t.Fatal("expected stack to be removed when no services remain")
	}
}

// --- onChange events ---

func TestCache_OnChange_AllTypes(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	svc := swarm.Service{ID: "svc1"}
	c.SetService(svc)
	c.DeleteService("svc1")

	c.SetTask(swarm.Task{ID: "t1"})
	c.DeleteTask("t1")

	cfg := swarm.Config{ID: "cfg1"}
	c.SetConfig(cfg)
	c.DeleteConfig("cfg1")

	sec := swarm.Secret{ID: "sec1"}
	c.SetSecret(sec)
	c.DeleteSecret("sec1")

	c.SetNetwork(network.Summary{ID: "net1"})
	c.DeleteNetwork("net1")

	c.SetVolume(volume.Volume{Name: "vol1"})
	c.DeleteVolume("vol1")

	// 6 resource types × 2 ops (set + delete) = 12 events
	if len(events) != 12 {
		t.Fatalf("expected 12 events, got %d", len(events))
	}

	expected := []struct {
		typ, action string
	}{
		{"service", "update"}, {"service", "remove"},
		{"task", "update"}, {"task", "remove"},
		{"config", "update"}, {"config", "remove"},
		{"secret", "update"}, {"secret", "remove"},
		{"network", "update"}, {"network", "remove"},
		{"volume", "update"}, {"volume", "remove"},
	}
	for i, exp := range expected {
		if events[i].Type != exp.typ || events[i].Action != exp.action {
			t.Errorf("event[%d]: expected %s/%s, got %s/%s", i, exp.typ, exp.action, events[i].Type, events[i].Action)
		}
	}
}

// --- List operations for remaining types ---

func TestCache_ListServices(t *testing.T) {
	c := New(nil)
	c.SetService(swarm.Service{ID: "s1"})
	c.SetService(swarm.Service{ID: "s2"})
	if svcs := c.ListServices(); len(svcs) != 2 {
		t.Fatalf("expected 2 services, got %d", len(svcs))
	}
}

func TestCache_ListTasks(t *testing.T) {
	c := New(nil)
	c.SetTask(swarm.Task{ID: "t1"})
	c.SetTask(swarm.Task{ID: "t2"})
	if tasks := c.ListTasks(); len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestCache_ListConfigs(t *testing.T) {
	c := New(nil)
	c.SetConfig(swarm.Config{ID: "c1"})
	c.SetConfig(swarm.Config{ID: "c2"})
	if cfgs := c.ListConfigs(); len(cfgs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(cfgs))
	}
}

func TestCache_ListSecrets(t *testing.T) {
	c := New(nil)
	c.SetSecret(swarm.Secret{ID: "s1"})
	c.SetSecret(swarm.Secret{ID: "s2"})
	if secs := c.ListSecrets(); len(secs) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secs))
	}
}

func TestCache_ListNetworks(t *testing.T) {
	c := New(nil)
	c.SetNetwork(network.Summary{ID: "n1"})
	c.SetNetwork(network.Summary{ID: "n2"})
	if nets := c.ListNetworks(); len(nets) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(nets))
	}
}

func TestCache_ListVolumes(t *testing.T) {
	c := New(nil)
	c.SetVolume(volume.Volume{Name: "v1"})
	c.SetVolume(volume.Volume{Name: "v2"})
	if vols := c.ListVolumes(); len(vols) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(vols))
	}
}

func TestSnapshot_ResourceTotals(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{
		ID:     "n1",
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{
				NanoCPUs:    4000000000, // 4 cores
				MemoryBytes: 8589934592, // 8 GB
			},
		},
	})
	c.SetNode(swarm.Node{
		ID:     "n2",
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{
				NanoCPUs:    2000000000, // 2 cores
				MemoryBytes: 4294967296, // 4 GB
			},
		},
	})

	snap := c.Snapshot()
	if snap.TotalCPU != 6 {
		t.Errorf("TotalCPU=%d, want 6", snap.TotalCPU)
	}
	if snap.TotalMemory != 12884901888 {
		t.Errorf("TotalMemory=%d, want 12884901888", snap.TotalMemory)
	}
}

func TestCache_ListStackSummaries(t *testing.T) {
	c := New(nil)

	// Service with 2 replicas, memory limit 512MB, CPU limit 0.5 cores
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "myapp_web",
				Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx"},
				Resources: &swarm.ResourceRequirements{
					Limits: &swarm.Limit{
						MemoryBytes: 512 * 1024 * 1024,
						NanoCPUs:    500_000_000,
					},
				},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: uint64Ptr(2)},
			},
		},
	})

	// Service mid-update
	c.SetService(swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "myapp_api",
				Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "api:latest"},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: uint64Ptr(1)},
			},
		},
		UpdateStatus: &swarm.UpdateStatus{State: swarm.UpdateStateUpdating},
	})

	// Tasks: 2 running for svc1, 1 failed for svc2
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t3", ServiceID: "svc2", Status: swarm.TaskStatus{State: swarm.TaskStateFailed}})

	// Config and network in same stack
	c.SetConfig(swarm.Config{ID: "cfg1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{
		Name: "myapp_config", Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
	}}})
	c.SetNetwork(network.Summary{ID: "net1", Name: "myapp_default", Labels: map[string]string{"com.docker.stack.namespace": "myapp"}})

	summaries := c.ListStackSummaries()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 stack summary, got %d", len(summaries))
	}

	s := summaries[0]
	if s.Name != "myapp" {
		t.Errorf("name=%q, want myapp", s.Name)
	}
	if s.ServiceCount != 2 {
		t.Errorf("serviceCount=%d, want 2", s.ServiceCount)
	}
	if s.DesiredTasks != 3 {
		t.Errorf("desiredTasks=%d, want 3", s.DesiredTasks)
	}
	if s.TasksByState["running"] != 2 {
		t.Errorf("running=%d, want 2", s.TasksByState["running"])
	}
	if s.TasksByState["failed"] != 1 {
		t.Errorf("failed=%d, want 1", s.TasksByState["failed"])
	}
	if s.UpdatingServices != 1 {
		t.Errorf("updatingServices=%d, want 1", s.UpdatingServices)
	}
	if s.MemoryLimitBytes != 2*512*1024*1024 {
		t.Errorf("memoryLimitBytes=%d, want %d", s.MemoryLimitBytes, 2*512*1024*1024)
	}
	if s.CPULimitCores != 1.0 {
		t.Errorf("cpuLimitCores=%f, want 1.0", s.CPULimitCores)
	}
	if s.ConfigCount != 1 {
		t.Errorf("configCount=%d, want 1", s.ConfigCount)
	}
	if s.NetworkCount != 1 {
		t.Errorf("networkCount=%d, want 1", s.NetworkCount)
	}
	if s.SecretCount != 0 {
		t.Errorf("secretCount=%d, want 0", s.SecretCount)
	}
	if s.VolumeCount != 0 {
		t.Errorf("volumeCount=%d, want 0", s.VolumeCount)
	}
	if s.MemoryUsageBytes != 0 {
		t.Errorf("memoryUsageBytes=%d, want 0 (populated by handler, not cache)", s.MemoryUsageBytes)
	}
	if s.CPUUsagePercent != 0 {
		t.Errorf("cpuUsagePercent=%f, want 0 (populated by handler, not cache)", s.CPUUsagePercent)
	}
}

//go:fix inline
func uint64Ptr(v uint64) *uint64 { return new(v) }

func TestReplaceAll_PartialSync(t *testing.T) {
	c := New(nil)
	// Pre-populate with a node
	c.SetNode(swarm.Node{ID: "existing"})

	// Partial sync — only services succeeded
	c.ReplaceAll(FullSyncData{
		Services:    []swarm.Service{{ID: "s1"}},
		HasServices: true,
		// HasNodes is false — nodes should be preserved
	})

	// Node should still exist
	if _, ok := c.GetNode("existing"); !ok {
		t.Error("existing node was lost during partial sync")
	}
	// Service should be updated
	if _, ok := c.GetService("s1"); !ok {
		t.Error("new service not found after partial sync")
	}
}

// --- ServicesUsing* cross-reference tests ---

func TestCache_ServicesUsingConfig(t *testing.T) {
	c := New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Configs: []*swarm.ConfigReference{{ConfigID: "cfg1", ConfigName: "app-config"}},
	}
	c.SetService(svc)

	svc2 := swarm.Service{ID: "svc2"}
	svc2.Spec.Name = "api"
	svc2.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	c.SetService(svc2)

	refs := c.ServicesUsingConfig("cfg1")
	if len(refs) != 1 {
		t.Fatalf("expected 1 service ref, got %d", len(refs))
	}
	if refs[0].ID != "svc1" || refs[0].Name != "web" {
		t.Errorf("unexpected ref: %+v", refs[0])
	}

	// No services reference this config
	refs = c.ServicesUsingConfig("nonexistent")
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for nonexistent config, got %d", len(refs))
	}
}

func TestCache_ServicesUsingSecret(t *testing.T) {
	c := New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Secrets: []*swarm.SecretReference{{SecretID: "sec1", SecretName: "tls-cert"}},
	}
	c.SetService(svc)

	svc2 := swarm.Service{ID: "svc2"}
	svc2.Spec.Name = "api"
	svc2.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	c.SetService(svc2)

	refs := c.ServicesUsingSecret("sec1")
	if len(refs) != 1 {
		t.Fatalf("expected 1 service ref, got %d", len(refs))
	}
	if refs[0].ID != "svc1" || refs[0].Name != "web" {
		t.Errorf("unexpected ref: %+v", refs[0])
	}

	refs = c.ServicesUsingSecret("nonexistent")
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for nonexistent secret, got %d", len(refs))
	}
}

func TestCache_ServicesUsingNetwork(t *testing.T) {
	c := New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{{Target: "net1"}}
	c.SetService(svc)

	svc2 := swarm.Service{ID: "svc2"}
	svc2.Spec.Name = "api"
	svc2.Spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{{Target: "net2"}}
	c.SetService(svc2)

	refs := c.ServicesUsingNetwork("net1")
	if len(refs) != 1 {
		t.Fatalf("expected 1 service ref, got %d", len(refs))
	}
	if refs[0].ID != "svc1" || refs[0].Name != "web" {
		t.Errorf("unexpected ref: %+v", refs[0])
	}

	refs = c.ServicesUsingNetwork("nonexistent")
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for nonexistent network, got %d", len(refs))
	}
}

func TestCache_ServicesUsingVolume(t *testing.T) {
	c := New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "db"
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: "data-vol"}},
	}
	c.SetService(svc)

	svc2 := swarm.Service{ID: "svc2"}
	svc2.Spec.Name = "web"
	svc2.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Mounts: []mount.Mount{{Type: mount.TypeBind, Source: "/host/path"}},
	}
	c.SetService(svc2)

	refs := c.ServicesUsingVolume("data-vol")
	if len(refs) != 1 {
		t.Fatalf("expected 1 service ref, got %d", len(refs))
	}
	if refs[0].ID != "svc1" || refs[0].Name != "db" {
		t.Errorf("unexpected ref: %+v", refs[0])
	}

	refs = c.ServicesUsingVolume("nonexistent")
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for nonexistent volume, got %d", len(refs))
	}
}

func TestCache_SetService_EmitsCrossRefEvents(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Configs: []*swarm.ConfigReference{{ConfigID: "cfg1", ConfigName: "app-config"}},
	}
	svc.Spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{{Target: "net1"}}
	c.SetService(svc)

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "service" || events[0].Action != "update" {
		t.Errorf("event[0]: expected service/update, got %s/%s", events[0].Type, events[0].Action)
	}
	refEvents := events[1:]
	hasConfig := false
	hasNetwork := false
	for _, e := range refEvents {
		if e.Action != "ref_changed" {
			t.Errorf("expected ref_changed action, got %s", e.Action)
		}
		if e.Type == "config" && e.ID == "cfg1" {
			hasConfig = true
		}
		if e.Type == "network" && e.ID == "net1" {
			hasNetwork = true
		}
	}
	if !hasConfig {
		t.Error("missing ref_changed event for config cfg1")
	}
	if !hasNetwork {
		t.Error("missing ref_changed event for network net1")
	}
}

func TestCache_SetService_RefChangeDiff(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Configs: []*swarm.ConfigReference{{ConfigID: "cfg1"}},
	}
	c.SetService(svc)
	events = nil

	svc2 := swarm.Service{ID: "svc1"}
	svc2.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Configs: []*swarm.ConfigReference{{ConfigID: "cfg2"}},
	}
	c.SetService(svc2)

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	refEvents := events[1:]
	ids := map[string]bool{}
	for _, e := range refEvents {
		if e.Type != "config" || e.Action != "ref_changed" {
			t.Errorf("expected config/ref_changed, got %s/%s", e.Type, e.Action)
		}
		ids[e.ID] = true
	}
	if !ids["cfg1"] || !ids["cfg2"] {
		t.Errorf("expected ref_changed for both cfg1 and cfg2, got %v", ids)
	}
}

func TestCache_DeleteService_EmitsCrossRefEvents(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Secrets: []*swarm.SecretReference{{SecretID: "sec1"}},
	}
	c.SetService(svc)
	events = nil

	c.DeleteService("svc1")

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "service" || events[0].Action != "remove" {
		t.Errorf("event[0]: expected service/remove, got %s/%s", events[0].Type, events[0].Action)
	}
	if events[1].Type != "secret" || events[1].Action != "ref_changed" || events[1].ID != "sec1" {
		t.Errorf("event[1]: expected secret/ref_changed/sec1, got %s/%s/%s", events[1].Type, events[1].Action, events[1].ID)
	}
}

func TestCache_SetService_NoRefChange_NoExtraEvents(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	c.SetService(swarm.Service{ID: "svc1"})

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
}

func TestSnapshot_ConvergenceAndReservations(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{
		ID: "n1", Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		Spec:        swarm.NodeSpec{Availability: swarm.NodeAvailabilityDrain},
		Description: swarm.NodeDescription{Resources: swarm.Resources{NanoCPUs: 4_000_000_000, MemoryBytes: 8_589_934_592}},
	})
	c.SetNode(swarm.Node{
		ID: "n2", Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		Spec:        swarm.NodeSpec{Availability: swarm.NodeAvailabilityActive},
		Description: swarm.NodeDescription{Resources: swarm.Resources{NanoCPUs: 4_000_000_000, MemoryBytes: 8_589_934_592}},
	})
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: new(uint64(2))}},
			TaskTemplate: swarm.TaskSpec{
				Resources: &swarm.ResourceRequirements{
					Reservations: &swarm.Resources{NanoCPUs: 500_000_000, MemoryBytes: 256 * 1024 * 1024},
				},
			},
		},
	})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetService(swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "api"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: new(uint64(3))}},
		},
	})
	c.SetTask(swarm.Task{ID: "t3", ServiceID: "svc2", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t4", ServiceID: "svc2", Status: swarm.TaskStatus{State: swarm.TaskStateFailed}})

	snap := c.Snapshot()
	if snap.ServicesConverged != 1 {
		t.Errorf("ServicesConverged=%d, want 1", snap.ServicesConverged)
	}
	if snap.ServicesDegraded != 1 {
		t.Errorf("ServicesDegraded=%d, want 1", snap.ServicesDegraded)
	}
	if snap.NodesDraining != 1 {
		t.Errorf("NodesDraining=%d, want 1", snap.NodesDraining)
	}
	if snap.ReservedCPU != 1_000_000_000 {
		t.Errorf("ReservedCPU=%d, want 1000000000", snap.ReservedCPU)
	}
	if snap.ReservedMemory != 512*1024*1024 {
		t.Errorf("ReservedMemory=%d, want %d", snap.ReservedMemory, 512*1024*1024)
	}
}
