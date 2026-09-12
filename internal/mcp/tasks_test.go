package mcp

import (
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
