package cluster

import (
	"context"
	"errors"
	"strconv"
	"strings"
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

// TestAwaitServiceRefusesToJudgeAStaleCache: the watcher fills the cache
// asynchronously, so at the moment a write returns a 5→2 scale asks 2 running
// against a desired 5 and would report converged with five replicas still up.
// Only the version gate can tell.
func TestAwaitServiceRefusesToJudgeAStaleCache(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Meta: swarm.Meta{Version: swarm.Version{Index: 5}},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: new(uint64)},
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// The write produced version 9; the cache is still at 5.
	_, err := AwaitService(ctx, c, "svc1", 9, 10*time.Millisecond, time.Second)
	if err == nil {
		t.Fatal("AwaitService reported convergence against a pre-mutation cache")
	}
}

func TestAwaitServiceReturnsOnceTheVersionCatchesUp(t *testing.T) {
	c := cache.New(nil)
	replicas := uint64(2)
	set := func(version uint64) {
		c.SetService(swarm.Service{
			ID:   "svc1",
			Meta: swarm.Meta{Version: swarm.Version{Index: version}},
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "web"},
				Mode: swarm.ServiceMode{
					Replicated: &swarm.ReplicatedService{Replicas: &replicas},
				},
			},
		})
	}
	set(5)
	for i := range 2 {
		c.SetTask(swarm.Task{
			ID:           "t" + strconv.Itoa(i),
			ServiceID:    "svc1",
			Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
			DesiredState: swarm.TaskStateRunning,
		})
	}

	go func() {
		time.Sleep(30 * time.Millisecond)
		set(9)
	}()

	progress, err := AwaitService(context.Background(), c, "svc1", 9,
		10*time.Millisecond, time.Second)
	if err != nil {
		t.Fatalf("AwaitService: %v", err)
	}
	if !strings.Contains(progress, "converged") {
		t.Errorf("progress = %q, want a converged message", progress)
	}
}

func TestAwaitServiceHonoursContextCancellation(t *testing.T) {
	c := cache.New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := AwaitService(ctx, c, "svc1", 1, 10*time.Millisecond, time.Minute); err == nil {
		t.Fatal("AwaitService ignored a cancelled context")
	}
}

// TestAwaitConvergenceReturnsWhenPredicateSatisfied is the whole point of the
// Tasks work: the wait completes when the cluster reaches the desired state,
// not when the Docker API call returned.
func TestAwaitConvergenceReturnsWhenPredicateSatisfied(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "svc-1", 3, 1)

	go func() {
		time.Sleep(60 * time.Millisecond)
		seedService(t, c, "svc-1", 3, 3)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := time.Now()

	if _, err := AwaitService(ctx, c, "svc-1", 0, 10*time.Millisecond, time.Second); err != nil {
		t.Fatalf("AwaitService: %v", err)
	}

	// A convergence reported before the background update landed would mean
	// the wait judged a stale, unconverged cache as done rather than actually
	// polling for the change.
	if elapsed := time.Since(started); elapsed < 40*time.Millisecond {
		t.Fatalf("returned after %s, before the service could have converged", elapsed)
	}
}

// TestAwaitConvergenceRespectsDeadline — a service that never converges (a bad
// image, an unsatisfiable placement constraint) must fail the wait rather than
// hang forever.
func TestAwaitConvergenceRespectsDeadline(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "svc-1", 3, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	progress, err := AwaitService(ctx, c, "svc-1", 0, 10*time.Millisecond, time.Minute)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if progress == "" {
		t.Error("progress should describe what was still outstanding")
	}
}

// TestAwaitConvergenceChecksBeforeWaiting keeps an already-satisfied mutation
// from paying a full poll interval — scaling to the count a service already has
// should finish at once.
func TestAwaitConvergenceChecksBeforeWaiting(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "svc-1", 2, 2)

	// A context with no time left at all: only a pre-loop check can succeed.
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	time.Sleep(time.Millisecond)

	if _, err := AwaitService(ctx, c, "svc-1", 0, time.Minute, time.Minute); err != nil {
		t.Fatalf("AwaitService: %v", err)
	}
}

// TestServiceConvergedComparesRunningToDesired pins the predicate itself.
func TestServiceConvergedComparesRunningToDesired(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "svc-1", 3 /* desired */, 1 /* running */)

	done, status := serviceConvergedAt(c, "svc-1", 0)
	if done {
		t.Error("reported converged with 1/3 replicas running")
	}

	if status == "" {
		t.Error("status should describe progress for the client to poll")
	}

	seedService(t, c, "svc-1", 3, 3)

	if done, _ := serviceConvergedAt(c, "svc-1", 0); !done {
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

	if done, status := serviceConvergedAt(c, "svc-1", 0); done {
		t.Errorf("reported converged during a rolling update (status %q)", status)
	}
}

// TestServiceConvergedHandlesUnknownService covers the window between the
// Docker write returning and the watcher's event reaching the cache.
func TestServiceConvergedHandlesUnknownService(t *testing.T) {
	c := cache.New(nil)

	done, status := serviceConvergedAt(c, "nope", 0)
	if done {
		t.Error("reported converged for a service the cache has never seen")
	}

	if status == "" {
		t.Error("status should say the service is not in the cache yet")
	}
}

// A scale-down must not report success while the replicas it removed are still
// running. The cache is filled asynchronously, so mid-write a 5-to-2 scale looks
// like two desired against five running, or five against five — so the wait
// refuses to judge anything older than the version the write produced.
func TestServiceConvergedWaitsForTheWriteToReachTheCache(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "svc-1", 5 /* desired */, 5 /* running */)

	// The version the scale-down returned; the cache has not seen it yet.
	const wrote = 12

	done, status := serviceConvergedAt(c, "svc-1", wrote)
	if done {
		t.Errorf(
			"reported converged from the pre-scale cache (status %q) — five "+
				"replicas are still up",
			status,
		)
	}

	if status == "" {
		t.Error("status should say the mutation is not visible yet")
	}

	// The watcher catches up: the new spec, but the surplus tasks are still
	// draining and still count.
	svc, _ := c.GetService("svc-1")
	svc.Version.Index = wrote
	replicas := uint64(2)
	svc.Spec.Mode.Replicated.Replicas = &replicas
	c.SetService(svc)

	if done, status := serviceConvergedAt(c, "svc-1", wrote); done {
		t.Errorf("reported converged with 5 of 2 replicas still running (%q)", status)
	}

	// The surplus drains.
	for _, id := range []string{"svc-1-task-c", "svc-1-task-d", "svc-1-task-e"} {
		task, ok := c.GetTask(id)
		if !ok {
			t.Fatalf("task %s missing from the cache", id)
		}

		task.DesiredState = swarm.TaskStateShutdown
		c.SetTask(task)
	}

	if done, status := serviceConvergedAt(c, "svc-1", wrote); !done {
		t.Errorf("did not report converged once the scale-down finished (%q)", status)
	}
}

// The gate must not hold a wait open forever: a service already at or past the
// written version is judged on the spot.
func TestServiceConvergedJudgesAServiceTheCacheHasCaughtUpTo(t *testing.T) {
	c := cache.New(nil)
	seedService(t, c, "svc-1", 2, 2)

	svc, _ := c.GetService("svc-1")
	svc.Version.Index = 7
	c.SetService(svc)

	if done, status := serviceConvergedAt(c, "svc-1", 7); !done {
		t.Errorf("did not report converged at exactly the written version (%q)", status)
	}

	if done, status := serviceConvergedAt(c, "svc-1", 6); !done {
		t.Errorf("did not report converged past the written version (%q)", status)
	}
}
