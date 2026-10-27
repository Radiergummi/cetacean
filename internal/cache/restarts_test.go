package cache

import (
	"fmt"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

func TestRestartTracker_RecordAndCount(t *testing.T) {
	rt := NewRestartTracker(7*24*time.Hour, time.Hour)
	now := time.Now()

	rt.Record("svc1", now)
	rt.Record("svc1", now.Add(-30*time.Minute))
	rt.Record("svc1", now.Add(-25*time.Hour))
	rt.Record("svc2", now)

	if got := rt.Count("svc1", 24*time.Hour); got != 2 {
		t.Errorf("svc1 over 24h: want 2, got %d", got)
	}

	if got := rt.Count("svc1", 7*24*time.Hour); got != 3 {
		t.Errorf("svc1 over 7d: want 3, got %d", got)
	}

	if got := rt.Count("svc2", 24*time.Hour); got != 1 {
		t.Errorf("svc2 over 24h: want 1, got %d", got)
	}

	if got := rt.Count("missing", 24*time.Hour); got != 0 {
		t.Errorf("missing service: want 0, got %d", got)
	}
}

// Count already ignores what lies past the horizon, so only the stored
// buckets show whether it was dropped rather than kept forever.
func TestRestartTracker_PruneBeyondHorizon(t *testing.T) {
	rt := NewRestartTracker(24*time.Hour, time.Hour)
	now := time.Now()

	rt.Record("svc1", now.Add(-48*time.Hour))
	rt.Record("svc1", now)

	if got := len(rt.Snapshot().Services["svc1"]); got != 1 {
		t.Errorf("buckets held = %d, want 1: the one past the horizon was kept", got)
	}
}

func TestRestartTracker_LookbackCappedAtHorizon(t *testing.T) {
	rt := NewRestartTracker(24*time.Hour, time.Hour)
	now := time.Now()

	rt.Record("svc1", now.Add(-2*time.Hour))

	if got := rt.Count("svc1", 7*24*time.Hour); got != 1 {
		t.Errorf("lookback > horizon: want 1, got %d", got)
	}
}

func TestRestartTracker_Forget(t *testing.T) {
	rt := NewRestartTracker(24*time.Hour, time.Hour)
	rt.Record("svc1", time.Now())
	rt.Forget("svc1")

	if got := rt.Count("svc1", 24*time.Hour); got != 0 {
		t.Errorf("after Forget: want 0, got %d", got)
	}
}

func TestRestartTracker_SnapshotRoundTrip(t *testing.T) {
	src := NewRestartTracker(24*time.Hour, time.Hour)
	now := time.Now()
	src.Record("svc1", now)
	src.Record("svc1", now.Add(-2*time.Hour))
	src.Record("svc2", now.Add(-30*time.Minute))

	snap := src.Snapshot()

	dst := NewRestartTracker(24*time.Hour, time.Hour)
	dst.Restore(snap)

	if got := dst.Count("svc1", 24*time.Hour); got != 2 {
		t.Errorf("restored svc1: want 2, got %d", got)
	}

	if got := dst.Count("svc2", 24*time.Hour); got != 1 {
		t.Errorf("restored svc2: want 1, got %d", got)
	}
}

func TestRestartTracker_RestorePrunesStale(t *testing.T) {
	now := time.Now()
	dst := NewRestartTracker(24*time.Hour, time.Hour)
	dst.Restore(RestartTrackerSnapshot{
		Horizon: 24 * time.Hour,
		Bucket:  time.Hour,
		Services: map[string]map[int64]uint32{"svc1": {
			dst.bucketKey(now.Add(-48 * time.Hour)): 1,
			dst.bucketKey(now):                      1,
		}},
	})

	if got := len(dst.Snapshot().Services["svc1"]); got != 1 {
		t.Errorf("buckets held = %d, want 1: a stale one survived the restore", got)
	}
}

func TestRestartTracker_ZeroTimestampFallsBackToNow(t *testing.T) {
	rt := NewRestartTracker(24*time.Hour, time.Hour)
	rt.Record("svc1", time.Time{})

	if got := rt.Count("svc1", 24*time.Hour); got != 1 {
		t.Errorf("zero timestamp: want 1, got %d", got)
	}
}

func TestIsFailureState(t *testing.T) {
	cases := []struct {
		state swarm.TaskState
		want  bool
	}{
		{swarm.TaskStateFailed, true},
		{swarm.TaskStateRejected, true},
		{swarm.TaskStateOrphaned, true},
		{swarm.TaskStateShutdown, false},
		{swarm.TaskStateComplete, false},
		{swarm.TaskStateRunning, false},
		{swarm.TaskStateStarting, false},
	}

	for _, c := range cases {
		if got := IsFailureState(c.state); got != c.want {
			t.Errorf("state %q: want %v, got %v", c.state, c.want, got)
		}
	}
}

func TestCache_SetTask_FailureTransitionCountedOnce(t *testing.T) {
	c := New(nil)
	now := time.Now()

	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning, Timestamp: now},
	})

	if got := c.RestartCount("svc1", 24*time.Hour); got != 0 {
		t.Fatalf("running task: want 0, got %d", got)
	}

	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateFailed, Timestamp: now.Add(time.Minute)},
	})

	if got := c.RestartCount("svc1", 24*time.Hour); got != 1 {
		t.Fatalf("after failure: want 1, got %d", got)
	}

	// Re-observation of the same already-failed task does not double count.
	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status: swarm.TaskStatus{
			State:     swarm.TaskStateFailed,
			Timestamp: now.Add(time.Minute),
			Message:   "different message",
		},
	})

	if got := c.RestartCount("svc1", 24*time.Hour); got != 1 {
		t.Fatalf("re-observation: want 1, got %d", got)
	}
}

func TestCache_SetTask_NewTaskInFailedStateCounted(t *testing.T) {
	c := New(nil)

	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateFailed, Timestamp: time.Now()},
	})

	if got := c.RestartCount("svc1", 24*time.Hour); got != 1 {
		t.Errorf("new failed task: want 1, got %d", got)
	}
}

func TestCache_SetTask_VoluntaryShutdownNotCounted(t *testing.T) {
	c := New(nil)
	now := time.Now()

	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning, Timestamp: now},
	})

	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status: swarm.TaskStatus{
			State:     swarm.TaskStateShutdown,
			Timestamp: now.Add(time.Minute),
		},
	})

	if got := c.RestartCount("svc1", 24*time.Hour); got != 0 {
		t.Errorf("voluntary shutdown: want 0, got %d", got)
	}
}

func TestCache_DeleteService_ForgetsRestartHistory(t *testing.T) {
	c := New(nil)

	c.SetService(
		swarm.Service{
			ID:   "svc1",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
		},
	)
	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateFailed, Timestamp: time.Now()},
	})

	if got := c.RestartCount("svc1", 24*time.Hour); got != 1 {
		t.Fatalf("setup: want 1, got %d", got)
	}

	c.DeleteService("svc1")

	if got := c.RestartCount("svc1", 24*time.Hour); got != 0 {
		t.Errorf("after DeleteService: want 0, got %d", got)
	}
}

func TestCache_ReplaceAll_DetectsNewFailures(t *testing.T) {
	c := New(nil)
	now := time.Now()

	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning, Timestamp: now},
	})

	c.ReplaceAll(FullSyncData{
		HasTasks: true,
		Tasks: []swarm.Task{
			{
				ID:        "t1",
				ServiceID: "svc1",
				Status: swarm.TaskStatus{
					State:     swarm.TaskStateFailed,
					Timestamp: now.Add(time.Minute),
				},
			},
		},
	})

	if got := c.RestartCount("svc1", 24*time.Hour); got != 1 {
		t.Errorf("ReplaceAll missed failure transition: want 1, got %d", got)
	}
}

func TestCache_ReplaceAll_ForgetsRemovedServices(t *testing.T) {
	c := New(nil)
	c.SetService(
		swarm.Service{
			ID:   "svc1",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
		},
	)
	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateFailed, Timestamp: time.Now()},
	})

	c.ReplaceAll(FullSyncData{HasServices: true, Services: nil})

	if got := c.RestartCount("svc1", 24*time.Hour); got != 0 {
		t.Errorf("expected forget on service removal, got %d", got)
	}
}

// The counts are labelled by the window they cover, but a tracker can only answer
// for as long as it has been watching, and it starts with the process. A young
// one reports the same figure for the hour and the week, which is the signal a
// reader uses to tell a new fault from a chronic one.
func TestRestartTrackerReportsHowFarBackItCanAccountFor(t *testing.T) {
	rt := NewRestartTracker(7*24*time.Hour, time.Hour)

	start := time.Now()
	since := rt.TrackingSince()

	if since.Before(start.Add(-time.Minute)) || since.After(time.Now()) {
		t.Errorf("a fresh tracker claims to reach back to %v; it has only just started", since)
	}

	// Restoring a snapshot moves the horizon back to the oldest bucket it
	// recovered: that history really is accounted for, and refusing to say so
	// would understate a tracker that did survive the restart.
	old := time.Now().Add(-48 * time.Hour)
	rt.Restore(RestartTrackerSnapshot{
		Horizon: 7 * 24 * time.Hour,
		Bucket:  time.Hour,
		Services: map[string]map[int64]uint32{
			"svc": {old.Truncate(time.Hour).Unix(): 4},
		},
	})

	if since := rt.TrackingSince(); since.After(old.Add(time.Hour)) {
		t.Errorf("TrackingSince = %v after restoring a bucket from %v", since, old)
	}
}

// The horizon can never reach further back than the retention window, however
// long the process has been up: buckets past it are pruned, so claiming to
// account for them would be the same lie in the other direction.
func TestRestartTrackerHorizonIsCappedByRetention(t *testing.T) {
	rt := NewRestartTracker(2*time.Hour, time.Hour)
	rt.Restore(RestartTrackerSnapshot{
		Horizon:  2 * time.Hour,
		Bucket:   time.Hour,
		Services: map[string]map[int64]uint32{"svc": {time.Now().Add(-72 * time.Hour).Unix(): 9}},
	})

	if since := rt.TrackingSince(); since.Before(time.Now().Add(-3 * time.Hour)) {
		t.Errorf("TrackingSince = %v, beyond the 2h retention horizon", since)
	}
}

// A crash loop reads "running" whenever a replacement is caught up, so the
// failure rate is the only steady signal; a fault that is over must clear.
func TestRestartTrackerFlapping(t *testing.T) {
	rt := NewRestartTracker(7*24*time.Hour, time.Hour)
	now := time.Now()

	for i := range flappingRestarts {
		rt.Record("loop", now.Add(-time.Duration(i)*5*time.Second))
	}

	// Newest first, as a full sync may report them, and all outside the window.
	for i := range flappingRestarts + 2 {
		rt.Record("over", now.Add(-flappingWindow-time.Duration(i+1)*time.Minute))
	}

	rt.Record("once", now)

	got := rt.Flapping()
	if !got["loop"] {
		t.Error("a service failing every five seconds is not flapping")
	}
	if got["over"] {
		t.Error("failures older than the window still count as flapping")
	}
	if got["once"] {
		t.Error("a single failure counts as flapping")
	}

	rt.Forget("loop")
	if rt.Flapping()["loop"] {
		t.Error("a forgotten service is still flapping")
	}
}

// An old failure arriving late must not displace a newer one: a loop that is
// still going stays flapping however its history is replayed.
func TestRestartTrackerFlappingKeepsNewest(t *testing.T) {
	rt := NewRestartTracker(7*24*time.Hour, time.Hour)
	now := time.Now()

	for i := range flappingRestarts {
		rt.Record("loop", now.Add(-time.Duration(i)*time.Second))
	}

	rt.Record("loop", now.Add(-time.Hour))

	if !rt.Flapping()["loop"] {
		t.Error("a late, older failure pushed the recent ones out")
	}
}

// Swarm keeps failed tasks for days, and the first sync replays them: those
// are history the tracker did not see, not failures since it began. A loop
// already running then must still read as flapping.
func TestCacheFirstSyncBackfillsOnlyTheFlappingWindow(t *testing.T) {
	c := New(nil)
	now := time.Now()

	tasks := []swarm.Task{failedTask("old", "old", now.Add(-72*time.Hour))}
	for i := range flappingRestarts {
		id := fmt.Sprintf("loop-%d", i)
		tasks = append(tasks, failedTask(id, "loop", now.Add(-time.Duration(i+1)*5*time.Second)))
	}
	c.ReplaceAll(FullSyncData{HasTasks: true, Tasks: tasks})

	if got := c.RestartCount("old", 7*24*time.Hour); got != 0 {
		t.Errorf("a failure the first sync found counted: got %d", got)
	}
	if got := c.RestartCount("loop", time.Hour); got != 0 {
		t.Errorf("failures the first sync found counted: got %d", got)
	}
	if !c.IsFlapping("loop") {
		t.Error("a loop the first sync found is not flapping")
	}
}

// A failure that appears in a later sync happened while the tracker watched,
// whatever the manager's clock stamped on it.
func TestCacheLaterSyncCountsAFailureStampedInThePast(t *testing.T) {
	c := New(nil)
	c.ReplaceAll(FullSyncData{HasTasks: true})

	skewed := time.Now().Add(-10 * time.Minute)
	c.ReplaceAll(FullSyncData{
		HasTasks: true,
		Tasks:    []swarm.Task{failedTask("t1", "svc1", skewed)},
	})

	if got := c.RestartCount("svc1", time.Hour); got != 1 {
		t.Errorf("a new failure behind our clock was dropped: got %d", got)
	}
}

func failedTask(id, serviceID string, at time.Time) swarm.Task {
	return swarm.Task{
		ID:        id,
		ServiceID: serviceID,
		Status:    swarm.TaskStatus{State: swarm.TaskStateFailed, Timestamp: at},
	}
}
