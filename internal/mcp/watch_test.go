package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// A service already in its desired state returns at once — the common case,
// and the one that must not cost a timeout.
func TestWatchReturnsImmediatelyWhenAlreadyConverged(t *testing.T) {
	c := cache.New(nil)
	one := uint64(1)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	})
	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.toolWatch(context.Background(), toolRequest(t, "watch", map[string]any{
		"service": "web",
		"timeout": float64(2),
	}))
	if err != nil {
		t.Fatalf("toolWatch: %v", err)
	}

	var got watchResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Outcome != "converged" {
		t.Errorf("outcome = %q, want converged", got.Outcome)
	}
}

// A service that never converges must report why, and must respect the
// caller's timeout rather than the five-minute default.
func TestWatchTimesOutWithTheLastProgressLine(t *testing.T) {
	c := cache.New(nil)
	three := uint64(3)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &three}},
		},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.toolWatch(context.Background(), toolRequest(t, "watch", map[string]any{
		"service": "web",
		"timeout": float64(1),
	}))
	if err != nil {
		t.Fatalf("toolWatch: %v", err)
	}

	var got watchResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Outcome != "timeout" {
		t.Errorf("outcome = %q, want timeout", got.Outcome)
	}
	if got.Observed == "" {
		t.Error("Observed is empty; a timeout must say how far it got")
	}
}

func TestWatchRejectsAnUnknownService(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	_, err := srv.toolWatch(context.Background(), toolRequest(t, "watch", map[string]any{
		"service": "nosuch",
	}))
	if err == nil {
		t.Fatal("expected an error for an unknown service")
	}
}
