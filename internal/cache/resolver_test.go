package cache

import (
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

func TestStackOf(t *testing.T) {
	c := New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "webapp",
				Labels: map[string]string{"com.docker.stack.namespace": "mystack"},
			},
		},
	})
	c.SetConfig(swarm.Config{
		ID: "cfg1",
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   "myconfig",
				Labels: map[string]string{"com.docker.stack.namespace": "mystack"},
			},
		},
	})

	// Lookup by name (not Docker ID) — this is what the ACL evaluator passes.
	if got := c.StackOf("service", "webapp"); got != "mystack" {
		t.Fatalf("StackOf(service, webapp) = %q, want mystack", got)
	}
	if got := c.StackOf("config", "myconfig"); got != "mystack" {
		t.Fatalf("StackOf(config, myconfig) = %q, want mystack", got)
	}
	if got := c.StackOf("service", "nonexistent"); got != "" {
		t.Fatalf("StackOf(service, nonexistent) = %q, want empty", got)
	}
	// Docker ID should NOT match — StackOf uses names.
	if got := c.StackOf("service", "svc1"); got != "" {
		t.Fatalf("StackOf(service, svc1) should not match by Docker ID, got %q", got)
	}
}

func TestServiceOfTask(t *testing.T) {
	c := New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})
	c.SetTask(swarm.Task{
		ID:        "task1",
		ServiceID: "svc1",
	})

	if got := c.ServiceOfTask("task1"); got != "webapp" {
		t.Fatalf("ServiceOfTask(task1) = %q, want webapp", got)
	}
	if got := c.ServiceOfTask("nonexistent"); got != "" {
		t.Fatalf("ServiceOfTask(nonexistent) = %q, want empty", got)
	}
}

func TestStackOf_Secrets(t *testing.T) {
	c := New(nil)
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   "mysecret",
				Labels: map[string]string{"com.docker.stack.namespace": "mystack"},
			},
		},
	})

	if got := c.StackOf("secret", "mysecret"); got != "mystack" {
		t.Fatalf("StackOf(secret, mysecret) = %q, want mystack", got)
	}
}

func TestStackOf_Networks(t *testing.T) {
	c := New(nil)
	c.SetNetwork(network.Summary{
		ID:     "net1",
		Name:   "mynet",
		Labels: map[string]string{"com.docker.stack.namespace": "mystack"},
	})

	if got := c.StackOf("network", "mynet"); got != "mystack" {
		t.Fatalf("StackOf(network, mynet) = %q, want mystack", got)
	}
}

func TestStackOf_Volumes(t *testing.T) {
	c := New(nil)
	c.SetVolume(volume.Volume{
		Name:   "myvol",
		Labels: map[string]string{"com.docker.stack.namespace": "mystack"},
	})

	if got := c.StackOf("volume", "myvol"); got != "mystack" {
		t.Fatalf("StackOf(volume, myvol) = %q, want mystack", got)
	}
}

func TestStackOf_ResourceNotInStack(t *testing.T) {
	c := New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "standalone"}},
	})

	if got := c.StackOf("service", "standalone"); got != "" {
		t.Fatalf("expected empty for service with no stack label, got %q", got)
	}
}

func TestStackOf_UnknownType(t *testing.T) {
	c := New(nil)
	if got := c.StackOf("unknown", "id1"); got != "" {
		t.Fatalf("expected empty for unknown type, got %q", got)
	}
}

func TestServiceOfTask_ServiceNotInCache(t *testing.T) {
	c := New(nil)
	c.SetTask(swarm.Task{
		ID:        "task2",
		ServiceID: "svc-missing",
	})

	if got := c.ServiceOfTask("task2"); got != "" {
		t.Fatalf("expected empty when service is missing, got %q", got)
	}
}

// StackOf answers for a service through a name index, so the index has to follow
// every write: a rename, a removal, and both bulk replacements.
func TestStackOf_ServiceFollowsEveryWrite(t *testing.T) {
	service := func(id, name, stack string) swarm.Service {
		return swarm.Service{ID: id, Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
			Name:   name,
			Labels: map[string]string{StackLabel: stack},
		}}}
	}

	c := New(nil)
	c.SetService(service("svc1", "web", "shop"))
	c.SetService(service("svc1", "web-renamed", "shop"))

	if got := c.StackOf("service", "web"); got != "" {
		t.Errorf("after rename, StackOf(old name) = %q, want empty", got)
	}
	if got := c.StackOf("service", "web-renamed"); got != "shop" {
		t.Errorf("after rename, StackOf(new name) = %q, want shop", got)
	}

	c.DeleteService("svc1")
	if got := c.StackOf("service", "web-renamed"); got != "" {
		t.Errorf("after delete, StackOf = %q, want empty", got)
	}

	c.ReplaceAll(FullSyncData{
		HasServices: true,
		Services:    []swarm.Service{service("svc2", "api", "billing")},
	})
	if got := c.StackOf("service", "api"); got != "billing" {
		t.Errorf("after a sync, StackOf(api) = %q, want billing", got)
	}

	c.ReplaceAll(
		FullSyncData{Services: []swarm.Service{service("svc3", "db", "data")}, HasServices: true},
	)
	if got := c.StackOf("service", "api"); got != "" {
		t.Errorf("after ReplaceAll, StackOf(dropped service) = %q, want empty", got)
	}
	if got := c.StackOf("service", "db"); got != "data" {
		t.Errorf("after ReplaceAll, StackOf(db) = %q, want data", got)
	}
}
