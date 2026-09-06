package cache

import (
	"maps"
	"sync"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

// RestartTracker counts involuntary task terminations per service in fixed-size
// time buckets. Used by the recommendation engine to flag flaky services without
// relying on Prometheus.
//
// A task is "failed" when its observed state transitions into TaskStateFailed,
// TaskStateRejected, or TaskStateOrphaned — these are the swarm states for
// involuntary terminations. Voluntary shutdowns (rolling updates, scale-down)
// use TaskStateShutdown and are intentionally not counted.
type RestartTracker struct {
	mu       sync.RWMutex
	horizon  time.Duration
	bucket   time.Duration
	services map[string]map[int64]uint32 // serviceID -> bucket-start-unix -> count

	// observing is the moment this tracker began counting: process start, or
	// the oldest bucket a restored snapshot brought back. It is what keeps
	// "since I started counting" from reading as "in the last seven days".
	observing time.Time
}

// NewRestartTracker creates a tracker with the given retention horizon and
// per-bucket granularity (e.g. 7d / 1h).
func NewRestartTracker(horizon, bucket time.Duration) *RestartTracker {
	if bucket <= 0 {
		bucket = time.Hour
	}

	if horizon < bucket {
		horizon = bucket
	}

	return &RestartTracker{
		horizon:   horizon,
		bucket:    bucket,
		services:  make(map[string]map[int64]uint32),
		observing: time.Now(),
	}
}

// TrackingSince is the earliest moment the tracker can account for, and so the
// start of the only window its counts honestly describe.
//
// A count is labelled by the window asked for, but the tracker is built at
// startup: a freshly-restarted Cetacean answered "107 failures over the past
// 7d" for a service that had failed some twenty thousand times over two days,
// and reported the identical figure for the hour and the week — which is
// precisely the comparison a reader uses to tell a new fault from a chronic
// one. Callers report this beside the counts so the two cases separate.
//
// It is the later of when this tracker started observing and the retention
// horizon, because buckets past the horizon are pruned: claiming to account
// for them would be the same misstatement in the other direction.
func (rt *RestartTracker) TrackingSince() time.Time {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	cutoff := time.Now().Add(-rt.horizon)
	if rt.observing.After(cutoff) {
		return rt.observing
	}

	return cutoff
}

// IsFailureState reports whether s is one of the involuntary terminal states.
func IsFailureState(s swarm.TaskState) bool {
	return s == swarm.TaskStateFailed ||
		s == swarm.TaskStateRejected ||
		s == swarm.TaskStateOrphaned
}

// IsTerminalState reports whether s is any terminal state — failures plus
// natural-completion paths. ContainerStatus.ExitCode is only meaningful here;
// for non-terminal states Docker often reports a placeholder like -1.
func IsTerminalState(s swarm.TaskState) bool {
	return IsFailureState(s) ||
		s == swarm.TaskStateComplete ||
		s == swarm.TaskStateShutdown ||
		s == swarm.TaskStateRemove
}

// Record increments the failure counter for serviceID at the given time and
// prunes buckets older than the horizon. A zero timestamp falls back to now.
func (rt *RestartTracker) Record(serviceID string, when time.Time) {
	if serviceID == "" {
		return
	}

	if when.IsZero() {
		when = time.Now()
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	buckets := rt.services[serviceID]
	if buckets == nil {
		buckets = make(map[int64]uint32)
		rt.services[serviceID] = buckets
	}

	key := rt.bucketKey(when)
	buckets[key]++

	rt.pruneLocked(buckets, time.Now())
}

// Count returns the total number of failures for serviceID within the given
// lookback, capped at the tracker's horizon.
func (rt *RestartTracker) Count(serviceID string, lookback time.Duration) uint64 {
	if lookback > rt.horizon {
		lookback = rt.horizon
	}

	rt.mu.RLock()
	defer rt.mu.RUnlock()

	buckets := rt.services[serviceID]
	if buckets == nil {
		return 0
	}

	cutoff := rt.bucketKey(time.Now().Add(-lookback))
	var total uint64
	for k, v := range buckets {
		if k >= cutoff {
			total += uint64(v)
		}
	}

	return total
}

// Forget drops all buckets for serviceID. Called when a service is deleted.
func (rt *RestartTracker) Forget(serviceID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.services, serviceID)
}

// Horizon returns the configured retention window.
func (rt *RestartTracker) Horizon() time.Duration { return rt.horizon }

// Bucket returns the configured bucket size.
func (rt *RestartTracker) Bucket() time.Duration { return rt.bucket }

// Snapshot returns a serializable copy of the tracker's current state.
func (rt *RestartTracker) Snapshot() RestartTrackerSnapshot {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	services := make(map[string]map[int64]uint32, len(rt.services))
	for id, buckets := range rt.services {
		clone := make(map[int64]uint32, len(buckets))
		maps.Copy(clone, buckets)
		services[id] = clone
	}

	return RestartTrackerSnapshot{
		Horizon:  rt.horizon,
		Bucket:   rt.bucket,
		Services: services,
	}
}

// Restore replaces the tracker's state from a snapshot, pruning stale buckets.
// Bucket size and horizon from the snapshot are honored only if compatible
// with the current tracker; mismatches keep the existing settings and the
// snapshot data is bucketed under those settings as-is (callers should ensure
// the snapshot was produced with matching settings).
func (rt *RestartTracker) Restore(snap RestartTrackerSnapshot) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.services = make(map[string]map[int64]uint32, len(snap.Services))
	now := time.Now()

	for id, buckets := range snap.Services {
		clone := make(map[int64]uint32, len(buckets))
		maps.Copy(clone, buckets)
		rt.services[id] = clone
		rt.pruneLocked(clone, now)

		// History that survived the restart really is accounted for, so the
		// horizon moves back to cover it. Refusing to would understate a
		// tracker that did persist, which is the opposite error.
		for bucket := range clone {
			if start := time.Unix(bucket, 0); start.Before(rt.observing) {
				rt.observing = start
			}
		}
	}
}

func (rt *RestartTracker) bucketKey(t time.Time) int64 {
	return t.Truncate(rt.bucket).Unix()
}

func (rt *RestartTracker) pruneLocked(buckets map[int64]uint32, now time.Time) {
	cutoff := rt.bucketKey(now.Add(-rt.horizon))
	for k := range buckets {
		if k < cutoff {
			delete(buckets, k)
		}
	}
}

// RestartTrackerSnapshot is the on-disk representation of a RestartTracker.
type RestartTrackerSnapshot struct {
	Horizon  time.Duration               `json:"horizon"`
	Bucket   time.Duration               `json:"bucket"`
	Services map[string]map[int64]uint32 `json:"services"`
}
