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
