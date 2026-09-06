package cluster_test

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

func TestDeriveServiceState(t *testing.T) {
	replicas := func(n uint64) *uint64 { return &n }

	tests := []struct {
		name         string
		svc          swarm.Service
		runningCount int
		want         string
	}{
		{
			name: "replicated running normally",
			svc: swarm.Service{
				Spec: swarm.ServiceSpec{
					Mode: swarm.ServiceMode{
						Replicated: &swarm.ReplicatedService{Replicas: replicas(3)},
					},
				},
			},
			runningCount: 3,
			want:         "running",
		},
		{
			name: "replicated partially running",
			svc: swarm.Service{
				Spec: swarm.ServiceSpec{
					Mode: swarm.ServiceMode{
						Replicated: &swarm.ReplicatedService{Replicas: replicas(3)},
					},
				},
			},
			runningCount: 1,
			want:         "pending",
		},
		{
			name: "replicated none running (failed)",
			svc: swarm.Service{
				Spec: swarm.ServiceSpec{
					Mode: swarm.ServiceMode{
						Replicated: &swarm.ReplicatedService{Replicas: replicas(3)},
					},
				},
			},
			runningCount: 0,
			want:         "failed",
		},
		{
			name: "updating",
			svc: swarm.Service{
				Spec: swarm.ServiceSpec{
					Mode: swarm.ServiceMode{
						Replicated: &swarm.ReplicatedService{Replicas: replicas(3)},
					},
				},
				UpdateStatus: &swarm.UpdateStatus{
					State: swarm.UpdateStateUpdating,
				},
			},
			runningCount: 2,
			want:         "updating",
		},
		{
			name: "global running",
			svc: swarm.Service{
				Spec: swarm.ServiceSpec{
					Mode: swarm.ServiceMode{
						Global: &swarm.GlobalService{},
					},
				},
			},
			runningCount: 2,
			want:         "running",
		},
		{
			name: "global no tasks (pending)",
			svc: swarm.Service{
				Spec: swarm.ServiceSpec{
					Mode: swarm.ServiceMode{
						Global: &swarm.GlobalService{},
					},
				},
			},
			runningCount: 0,
			want:         "pending",
		},
		{
			name:         "replicated zero desired zero running",
			svc:          swarm.Service{},
			runningCount: 0,
			want:         "running",
		},
		{
			name: "global updating",
			svc: swarm.Service{
				Spec: swarm.ServiceSpec{
					Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}},
				},
				UpdateStatus: &swarm.UpdateStatus{State: swarm.UpdateStateUpdating},
			},
			runningCount: 2,
			want:         "updating",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cluster.DeriveServiceState(tc.svc, tc.runningCount)
			if got != tc.want {
				t.Errorf("DeriveServiceState() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestServiceConvergedWaitsOutRollback is the reason ServiceConverged and
// DeriveServiceState share serviceUpdateInFlight. Swarm reports a rollback as
// "rollback_started", and during one the replica count still matches, because
// the old tasks are the ones being restored. A convergence check that only
// looked for "updating" would call a rollback done the instant it began —
// exactly the tool that most needs to wait.
func TestServiceConvergedWaitsOutRollback(t *testing.T) {
	replicas := uint64(2)

	for _, state := range []swarm.UpdateState{
		swarm.UpdateStateUpdating,
		swarm.UpdateStateRollbackStarted,
		swarm.UpdateStateRollbackPaused,
	} {
		t.Run(string(state), func(t *testing.T) {
			svc := swarm.Service{
				Spec: swarm.ServiceSpec{
					Mode: swarm.ServiceMode{
						Replicated: &swarm.ReplicatedService{Replicas: &replicas},
					},
				},
				UpdateStatus: &swarm.UpdateStatus{State: state},
			}

			if done, status := cluster.ServiceConverged(svc, 2); done {
				t.Errorf("reported converged during %s (status %q)", state, status)
			}

			if got := cluster.DeriveServiceState(svc, 2); got != "updating" {
				t.Errorf("DeriveServiceState = %q, want %q — the two must agree", got, "updating")
			}
		})
	}
}

// A wait that can never terminate is a worse failure than one that ends a
// moment early. The running count comes from a cache the watcher fills
// asynchronously, so it can briefly exceed the desired one — and before the
// count learned to ignore tasks destined for shutdown it did so for the whole
// five minutes between full re-syncs, reporting "waiting: 3/2 replicas
// running" for a rolling update Docker had already marked completed. Requiring
// equality made that unrecoverable; the surplus a scale-down leaves behind is
// already excluded by the count itself, which is what equality was guarding.
func TestServiceConvergedWithSurplusRunningTasks(t *testing.T) {
	svc := swarm.Service{
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: new(uint64(2))},
			},
		},
		UpdateStatus: &swarm.UpdateStatus{State: swarm.UpdateStateCompleted},
	}

	converged, observed := cluster.ServiceConverged(svc, 3)
	if !converged {
		t.Errorf("ServiceConverged(desired 2, running 3) = false, %q; want converged", observed)
	}

	// This replaces TestServiceConvergedRequiresExactCount, which pinned the
	// opposite. Its rationale — that a scale-down is not done while surplus
	// tasks are reaped — is now enforced a layer down, by the replica count
	// itself: Swarm marks a reaped task for shutdown, and
	// cache.countsAsRunningReplica drops it, so an overshoot here means the
	// cache is wrong rather than the service being mid-scale-down. The two
	// functions still have to agree on what "enough replicas" means.
	if got := cluster.DeriveServiceState(svc, 3); got != "running" {
		t.Errorf("DeriveServiceState = %q, want %q — the two must agree", got, "running")
	}
}

// An update still in flight outranks the count: a start-first rollout has both
// the outgoing and incoming task running at once, and reporting that as
// settled would call a deploy done before it is.
func TestServiceConvergedWaitsOutAnUpdateEvenWithEnoughRunning(t *testing.T) {
	svc := swarm.Service{
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: new(uint64(2))},
			},
		},
		UpdateStatus: &swarm.UpdateStatus{State: swarm.UpdateStateUpdating},
	}

	if converged, _ := cluster.ServiceConverged(svc, 3); converged {
		t.Error("ServiceConverged reported an in-flight update as settled")
	}
}
