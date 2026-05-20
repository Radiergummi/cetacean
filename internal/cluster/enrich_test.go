package cluster_test

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

func newTestCache() *cache.Cache {
	return cache.New(nil)
}

func TestEnrichTask(t *testing.T) {
	c := newTestCache()

	svc := swarm.Service{}
	svc.ID = "svc1"
	svc.Spec.Name = "my-service"
	c.SetService(svc)

	node := swarm.Node{}
	node.ID = "node1"
	node.Description.Hostname = "worker-1"
	c.SetNode(node)

	task := swarm.Task{}
	task.ID = "task1"
	task.ServiceID = "svc1"
	task.NodeID = "node1"

	et := cluster.EnrichTask(c, task)

	if et.ServiceName != "my-service" {
		t.Errorf("ServiceName = %q, want %q", et.ServiceName, "my-service")
	}
	if et.NodeHostname != "worker-1" {
		t.Errorf("NodeHostname = %q, want %q", et.NodeHostname, "worker-1")
	}
	if et.ID != "task1" {
		t.Errorf("Task.ID = %q, want %q", et.ID, "task1")
	}
}

func TestEnrichTaskMissingService(t *testing.T) {
	c := newTestCache()

	task := swarm.Task{}
	task.ID = "task1"
	task.ServiceID = "nonexistent-svc"
	task.NodeID = ""

	et := cluster.EnrichTask(c, task)

	if et.ServiceName != "" {
		t.Errorf("ServiceName = %q, want empty string", et.ServiceName)
	}
	if et.ID != "task1" {
		t.Errorf("Task.ID = %q, want %q", et.ID, "task1")
	}
}

func TestEnrichTaskMissingNode(t *testing.T) {
	c := newTestCache()

	svc := swarm.Service{}
	svc.ID = "svc2"
	svc.Spec.Name = "another-service"
	c.SetService(svc)

	task := swarm.Task{}
	task.ID = "task2"
	task.ServiceID = "svc2"
	task.NodeID = "nonexistent-node"

	et := cluster.EnrichTask(c, task)

	if et.ServiceName != "another-service" {
		t.Errorf("ServiceName = %q, want %q", et.ServiceName, "another-service")
	}
	if et.NodeHostname != "" {
		t.Errorf("NodeHostname = %q, want empty string", et.NodeHostname)
	}
}

func TestEnrichTasksSlice(t *testing.T) {
	c := newTestCache()

	for _, name := range []string{"alpha", "beta", "gamma"} {
		svc := swarm.Service{}
		svc.ID = name + "-svc"
		svc.Spec.Name = name
		c.SetService(svc)

		node := swarm.Node{}
		node.ID = name + "-node"
		node.Description.Hostname = name + "-host"
		c.SetNode(node)
	}

	tasks := []swarm.Task{
		{ID: "t1", ServiceID: "alpha-svc", NodeID: "alpha-node"},
		{ID: "t2", ServiceID: "beta-svc", NodeID: "beta-node"},
		{ID: "t3", ServiceID: "gamma-svc", NodeID: "gamma-node"},
	}

	enriched := cluster.EnrichTasks(c, tasks)

	if len(enriched) != 3 {
		t.Fatalf("len(enriched) = %d, want 3", len(enriched))
	}

	for i, tc := range []struct {
		serviceName  string
		nodeHostname string
	}{
		{"alpha", "alpha-host"},
		{"beta", "beta-host"},
		{"gamma", "gamma-host"},
	} {
		if enriched[i].ServiceName != tc.serviceName {
			t.Errorf("[%d] ServiceName = %q, want %q", i, enriched[i].ServiceName, tc.serviceName)
		}
		if enriched[i].NodeHostname != tc.nodeHostname {
			t.Errorf("[%d] NodeHostname = %q, want %q", i, enriched[i].NodeHostname, tc.nodeHostname)
		}
	}
}

func TestRedactSecret(t *testing.T) {
	original := swarm.Secret{}
	original.ID = "sec1"
	original.Spec.Name = "my-secret"
	original.Spec.Data = []byte("super-secret-value")

	redacted := cluster.RedactSecret(original)

	if redacted.Spec.Data != nil {
		t.Errorf("redacted.Spec.Data = %v, want nil", redacted.Spec.Data)
	}
	// Original must not be mutated.
	if original.Spec.Data == nil {
		t.Error("original.Spec.Data was mutated to nil")
	}
	if string(original.Spec.Data) != "super-secret-value" {
		t.Errorf("original.Spec.Data = %q, want original value", original.Spec.Data)
	}
	if redacted.ID != "sec1" {
		t.Errorf("redacted.ID = %q, want %q", redacted.ID, "sec1")
	}
}

func TestRedactSecrets(t *testing.T) {
	secrets := []swarm.Secret{
		{Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "s1"}, Data: []byte("data1")}},
		{Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "s2"}, Data: []byte("data2")}},
	}
	secrets[0].ID = "id1"
	secrets[1].ID = "id2"

	redacted := cluster.RedactSecrets(secrets)

	if len(redacted) != 2 {
		t.Fatalf("len(redacted) = %d, want 2", len(redacted))
	}
	for i, r := range redacted {
		if r.Spec.Data != nil {
			t.Errorf("[%d] Spec.Data = %v, want nil", i, r.Spec.Data)
		}
	}
	// Original slice must not be mutated.
	for i, s := range secrets {
		if s.Spec.Data == nil {
			t.Errorf("original[%d].Spec.Data was mutated to nil", i)
		}
	}
}
