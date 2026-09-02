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
// DeriveServiceState share ServiceUpdateInFlight. Swarm reports a rollback as
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

// TestServiceConvergedRequiresExactCount pins the deliberate difference from
// DeriveServiceState: a scale-down is not done while surplus tasks are still
// being reaped, even though the service is already "running" to a reader.
func TestServiceConvergedRequiresExactCount(t *testing.T) {
	replicas := uint64(2)
	svc := swarm.Service{
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
		},
	}

	if done, _ := cluster.ServiceConverged(svc, 5); done {
		t.Error("reported converged with 5 running and 2 desired")
	}

	if got := cluster.DeriveServiceState(svc, 5); got != "running" {
		t.Errorf("DeriveServiceState = %q, want %q", got, "running")
	}
}
