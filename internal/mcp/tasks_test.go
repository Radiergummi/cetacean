package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

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
	converge := func() (bool, string) {
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

	never := func() (bool, string) {
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

	always := func() (bool, string) {
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

	done, status := serviceConverged(c, "svc-1")()
	if done {
		t.Error("reported converged with 1/3 replicas running")
	}

	if status == "" {
		t.Error("status should describe progress for the client to poll")
	}

	seedService(t, c, "svc-1", 3, 3)

	if done, _ := serviceConverged(c, "svc-1")(); !done {
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

	if done, status := serviceConverged(c, "svc-1")(); done {
		t.Errorf("reported converged during a rolling update (status %q)", status)
	}
}

// TestServiceConvergedHandlesUnknownService covers the window between the
// Docker write returning and the watcher's event reaching the cache.
func TestServiceConvergedHandlesUnknownService(t *testing.T) {
	c := cache.New(nil)

	done, status := serviceConverged(c, "nope")()
	if done {
		t.Error("reported converged for a service the cache has never seen")
	}

	if status == "" {
		t.Error("status should say the service is not in the cache yet")
	}
}

// ttlOf reads the TTL a bounding left behind, so a test can assert on a value
// rather than on a pointer.
func ttlOf(t *testing.T, task *mcplib.TaskParams) int64 {
	t.Helper()

	if task == nil {
		t.Fatal("task params are nil")
	}

	if task.TTL == nil {
		t.Fatal("task TTL is nil; the server should have filled one in")
	}

	return *task.TTL
}

// taskParams builds the params of a task-augmented call. A nil ttl is the
// shape a client sends when it omits the field entirely.
func taskParams(ttl *int64) *mcplib.TaskParams {
	return &mcplib.TaskParams{TTL: ttl}
}

// TestBoundTaskTTLFillsInAMissingTTL is the whole point: mcp-go only schedules
// cleanup when the client supplied a TTL, so a client that omits one pins its
// result for the life of the process.
func TestBoundTaskTTLFillsInAMissingTTL(t *testing.T) {
	task := taskParams(nil)

	if clamped := boundTaskTTL(task, 15*time.Minute, time.Hour); clamped {
		t.Error("filling in a missing TTL reported a clamp")
	}

	if got := ttlOf(t, task); got != (15 * time.Minute).Milliseconds() {
		t.Errorf("TTL = %dms, want %dms", got, (15 * time.Minute).Milliseconds())
	}
}

// TestBoundTaskTTLTreatsNonPositiveAsMissing covers a client that sends the
// field but zeroes it. mcp-go only starts cleanup for ttl > 0, so zero leaks
// exactly as an absent field does and must be treated the same way.
func TestBoundTaskTTLTreatsNonPositiveAsMissing(t *testing.T) {
	for _, ttl := range []int64{0, -1} {
		task := taskParams(new(ttl))

		boundTaskTTL(task, 15*time.Minute, time.Hour)

		if got := ttlOf(t, task); got != (15 * time.Minute).Milliseconds() {
			t.Errorf("ttl %d: TTL = %dms, want the default %dms",
				ttl, got, (15 * time.Minute).Milliseconds())
		}
	}
}

// TestBoundTaskTTLClampsAnOverLongTTL is what makes this a bound rather than a
// default: without it a client needs one careless number, not an omission, to
// pin a result indefinitely.
func TestBoundTaskTTLClampsAnOverLongTTL(t *testing.T) {
	task := taskParams(new((30 * 24 * time.Hour).Milliseconds()))

	if clamped := boundTaskTTL(task, 15*time.Minute, time.Hour); !clamped {
		t.Error("clamping a 30-day TTL did not report a clamp")
	}

	if got := ttlOf(t, task); got != time.Hour.Milliseconds() {
		t.Errorf("TTL = %dms, want the maximum %dms", got, time.Hour.Milliseconds())
	}
}

// TestBoundTaskTTLLeavesAWorkableTTLAlone — a client asking for something
// inside the bounds is honoured verbatim.
func TestBoundTaskTTLLeavesAWorkableTTLAlone(t *testing.T) {
	want := (10 * time.Minute).Milliseconds()
	task := taskParams(new(want))

	if clamped := boundTaskTTL(task, 15*time.Minute, time.Hour); clamped {
		t.Error("a TTL inside the maximum reported a clamp")
	}

	if got := ttlOf(t, task); got != want {
		t.Errorf("TTL = %dms, want %dms untouched", got, want)
	}
}

// TestBoundTaskTTLIgnoresAPlainCall is the regression guard that matters most:
// inventing task params for a call that carried none would turn every ordinary
// synchronous tools/call into a task, which mcp-go answers with a task handle
// instead of the tool's result.
func TestBoundTaskTTLIgnoresAPlainCall(t *testing.T) {
	if clamped := boundTaskTTL(nil, 15*time.Minute, time.Hour); clamped {
		t.Error("a call with no task augmentation reported a clamp")
	}
}

// TestBoundTaskTTLZeroDisablesEachHalf — zero means "no policy" for both
// settings, matching how consent_ttl already reads in this config.
func TestBoundTaskTTLZeroDisablesEachHalf(t *testing.T) {
	missing := taskParams(nil)
	boundTaskTTL(missing, 0, time.Hour)

	if missing.TTL != nil {
		t.Errorf("a zero default filled in %dms; it should leave the TTL absent", *missing.TTL)
	}

	long := (30 * 24 * time.Hour).Milliseconds()
	explicit := taskParams(new(long))
	boundTaskTTL(explicit, 15*time.Minute, 0)

	if got := ttlOf(t, explicit); got != long {
		t.Errorf("a zero maximum clamped to %dms; it should leave the TTL alone", got)
	}
}

// TestBoundTaskTTLClampsItsOwnDefault covers a misconfiguration — a default
// above the maximum — rather than letting the fill-in escape the ceiling.
func TestBoundTaskTTLClampsItsOwnDefault(t *testing.T) {
	task := taskParams(nil)

	boundTaskTTL(task, 2*time.Hour, time.Hour)

	if got := ttlOf(t, task); got != time.Hour.Milliseconds() {
		t.Errorf("TTL = %dms, want the maximum %dms", got, time.Hour.Milliseconds())
	}
}
