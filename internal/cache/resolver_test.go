package cache

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func TestStackOf(t *testing.T) {
	c := New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp", Labels: map[string]string{"com.docker.stack.namespace": "mystack"}}},
	})
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "myconfig", Labels: map[string]string{"com.docker.stack.namespace": "mystack"}}},
	})

	if got := c.StackOf("service", "svc1"); got != "mystack" {
		t.Fatalf("expected mystack, got %q", got)
	}
	if got := c.StackOf("config", "cfg1"); got != "mystack" {
		t.Fatalf("expected mystack, got %q", got)
	}
	if got := c.StackOf("service", "nonexistent"); got != "" {
		t.Fatalf("expected empty, got %q", got)
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
		t.Fatalf("expected webapp, got %q", got)
	}
	if got := c.ServiceOfTask("nonexistent"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
