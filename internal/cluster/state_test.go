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

// The reason ServiceConverged and DeriveServiceState share
// serviceUpdateInFlight. Swarm reports a rollback as "rollback_started", and
// the replica count still matches during one, since the old tasks are what is
// being restored — so a check looking only for "updating" calls it done at once.
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

// A scale-down is not done while the replicas it removed are still running.
// Accepting any count that *reached* the desired one returns on the first look
// of every scale-down, since the surplus is by definition still up: a 5-to-2
// scale answers "converged: 5/2 replicas running".
func TestServiceConvergedRejectsSurplusRunningTasks(t *testing.T) {
	svc := swarm.Service{
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: new(uint64(2))},
			},
		},
		UpdateStatus: &swarm.UpdateStatus{State: swarm.UpdateStateCompleted},
	}

	converged, observed := cluster.ServiceConverged(svc, 3)
	if converged {
		t.Errorf(
			"ServiceConverged(desired 2, running 3) = true, %q; want not converged "+
				"— the third replica is still up",
			observed,
		)
	}

	if converged, observed := cluster.ServiceConverged(svc, 2); !converged {
		t.Errorf("ServiceConverged(desired 2, running 2) = false, %q; want converged", observed)
	}

	// DeriveServiceState deliberately does not follow it here. It answers
	// "is this service healthy", and a surplus replica is not a fault; only
	// "has the mutation landed" cares that the count is above the spec.
	if got := cluster.DeriveServiceState(svc, 3); got != "running" {
		t.Errorf("DeriveServiceState = %q, want %q", got, "running")
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
