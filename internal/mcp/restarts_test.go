package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// TestDescribeServiceReportsRestartsFromTheTracker drives the whole read path
// a caller uses — describe over the real cache — to prove the restart counts
// reach the digest. The cluster-level test pins the rule; this one pins the
// wiring, which is the half that silently goes missing.
//
// The scenario is the one the live evaluation hit: a service crash-looping
// every few seconds, whose digest said "running" with one failure because
// every earlier replica had already been replaced.
func TestDescribeServiceReportsRestartsFromTheTracker(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "demo_flaky"}},
	})

	// Eight replicas came up and died; the ninth is up right now, which is why
	// the derived state reads "running".
	now := time.Now()
	for i, id := range []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8"} {
		c.SetTask(swarm.Task{
			ID:        id,
			ServiceID: "svc1",
			Slot:      1,
			Status: swarm.TaskStatus{
				State:     swarm.TaskStateFailed,
				Timestamp: now.Add(-time.Duration(i) * time.Minute),
			},
		})
	}
	c.SetTask(swarm.Task{
		ID:        "t9",
		ServiceID: "svc1",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning, Timestamp: now},
	})

	srv := newResourceTestServer(t, c)

	digest, err := srv.digestOf(context.Background(), "service", mustService(t, c, "svc1"))
	if err != nil {
		t.Fatalf("digestOf: %v", err)
	}

	if digest.State != "running" {
		t.Logf("state = %q (the derived state is not what carries the loop)", digest.State)
	}

	if digest.Restarts == nil {
		t.Fatal(
			"Restarts is nil; the loop is invisible in the one call describe is meant to answer with",
		)
	}
	if digest.Restarts.LastHour != 8 {
		t.Errorf("LastHour = %d, want 8", digest.Restarts.LastHour)
	}
	if digest.Restarts.LastWeek != 8 {
		t.Errorf("LastWeek = %d, want 8", digest.Restarts.LastWeek)
	}
}

// TestDescribeNonServiceHasNoRestarts keeps the field off the seven types it
// does not apply to, so it never marshals a misleading zero.
func TestDescribeNonServiceHasNoRestarts(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://nodes/node1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := got["restarts"]; ok {
		t.Error("node digest carries a restarts field; it applies to services only")
	}
}

func mustService(t *testing.T, c *cache.Cache, id string) swarm.Service {
	t.Helper()

	svc, ok := c.GetService(id)
	if !ok {
		t.Fatalf("service %q not in cache", id)
	}

	return svc
}
