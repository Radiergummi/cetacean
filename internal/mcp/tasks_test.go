package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// seedService puts a replicated service and the tasks backing it into the
// cache, so a convergence predicate has something real to read.
func seedService(t *testing.T, c *cache.Cache, id string, desired, running int) {
	t.Helper()

	replicas := uint64(desired)

	c.SetService(swarm.Service{
		ID: id,
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: id},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	})

	for index := range desired {
		state := swarm.TaskStatePending
		if index < running {
			state = swarm.TaskStateRunning
		}

		c.SetTask(swarm.Task{
			ID:        id + "-task-" + string(rune('a'+index)),
			ServiceID: id,
			Status:    swarm.TaskStatus{State: state},
		})
	}
}

// TestAwaitConvergenceReturnsWhenPredicateSatisfied is the whole point of the
// Tasks work: the task completes when the cluster reaches the desired state,
// not when the Docker API call returned.
func TestAwaitConvergenceReturnsWhenPredicateSatisfied(t *testing.T) {
	srv := newTestServer(t)

	calls := 0
	converge := func(context.Context, *cache.Cache) (bool, string) {
		calls++

		return calls >= 3, "waiting for replicas"
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := srv.awaitConvergence(ctx, converge); err != nil {
		t.Fatalf("awaitConvergence: %v", err)
	}

	if calls < 3 {
		t.Fatalf("predicate called %d times, want at least 3", calls)
	}
}

// TestAwaitConvergenceRespectsDeadline — a service that never converges (a bad
// image, an unsatisfiable placement constraint) must fail the task rather than
// hang forever.
func TestAwaitConvergenceRespectsDeadline(t *testing.T) {
	srv := newTestServer(t)

	never := func(context.Context, *cache.Cache) (bool, string) {
		return false, "never converges"
	}

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	err := srv.awaitConvergence(ctx, never)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

// TestAwaitConvergenceChecksBeforeWaiting keeps an already-satisfied mutation
// from paying a full poll interval — scaling to the count a service already has
// should finish at once.
func TestAwaitConvergenceChecksBeforeWaiting(t *testing.T) {
	srv := newTestServer(t)

	always := func(context.Context, *cache.Cache) (bool, string) {
		return true, "already there"
	}

	// A context with no time left at all: only a pre-loop check can succeed.
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()

	time.Sleep(time.Millisecond)

	if err := srv.awaitConvergence(ctx, always); err != nil {
		t.Fatalf("awaitConvergence: %v", err)
	}
}

// TestServiceConvergedComparesRunningToDesired pins the predicate itself.
func TestServiceConvergedComparesRunningToDesired(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "svc-1", 3 /* desired */, 1 /* running */)

	done, status := serviceConverged("svc-1")(t.Context(), c)
	if done {
		t.Error("reported converged with 1/3 replicas running")
	}

	if status == "" {
		t.Error("status should describe progress for the client to poll")
	}

	seedService(t, c, "svc-1", 3, 3)

	if done, _ := serviceConverged("svc-1")(t.Context(), c); !done {
		t.Error("did not report converged with 3/3 replicas running")
	}
}

// TestServiceConvergedWaitsOutRollingUpdate stops a transient match from being
// reported as success: mid-rollout the running count passes through the target
// while the old tasks are still being replaced.
func TestServiceConvergedWaitsOutRollingUpdate(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "svc-1", 2, 2)

	svc, _ := c.GetService("svc-1")
	svc.UpdateStatus = &swarm.UpdateStatus{State: swarm.UpdateStateUpdating}
	c.SetService(svc)

	if done, status := serviceConverged("svc-1")(t.Context(), c); done {
		t.Errorf("reported converged during a rolling update (status %q)", status)
	}
}

// TestServiceConvergedHandlesUnknownService covers the window between the
// Docker write returning and the watcher's event reaching the cache.
func TestServiceConvergedHandlesUnknownService(t *testing.T) {
	c := cache.New(nil)

	done, status := serviceConverged("nope")(t.Context(), c)
	if done {
		t.Error("reported converged for a service the cache has never seen")
	}

	if status == "" {
		t.Error("status should say the service is not in the cache yet")
	}
}
