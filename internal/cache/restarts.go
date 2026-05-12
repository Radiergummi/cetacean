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
		horizon:  horizon,
		bucket:   bucket,
		services: make(map[string]map[int64]uint32),
	}
}

// IsFailureState reports whether s is one of the involuntary terminal states.
func IsFailureState(s swarm.TaskState) bool {
	return s == swarm.TaskStateFailed ||
		s == swarm.TaskStateRejected ||
		s == swarm.TaskStateOrphaned
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
