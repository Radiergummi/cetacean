package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// The point of the landing call: it names what is wrong. A count forces the
// caller to spend a second call finding out which, which is where the token
// budget actually goes.
func TestBuildClusterStatusNamesUnhealthyServices(t *testing.T) {
	one := uint64(1)
	broken := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "demo_stuck"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	}
	healthy := swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	}

	got := BuildClusterStatus(
		cache.ClusterSnapshot{},
		[]swarm.Service{broken, healthy},
		nil,
		map[string]int{"svc2": 1},
	)

	if len(got.UnhealthyServices) != 1 {
		t.Fatalf("unhealthy = %d, want 1", len(got.UnhealthyServices))
	}
	if got.UnhealthyServices[0].Name != "demo_stuck" {
		t.Errorf("name = %q, want demo_stuck", got.UnhealthyServices[0].Name)
	}
	if got.Healthy {
		t.Error("Healthy is true with a failed service")
	}
}

func TestBuildClusterStatusReportsHealthyWhenNothingIsWrong(t *testing.T) {
	one := uint64(1)
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	}

	got := BuildClusterStatus(
		cache.ClusterSnapshot{},
		[]swarm.Service{svc},
		nil,
		map[string]int{"svc1": 1},
	)

	if !got.Healthy {
		t.Errorf("Healthy = false with nothing wrong: %+v", got)
	}
	if len(got.UnhealthyServices) != 0 {
		t.Errorf("unhealthy = %+v, want empty", got.UnhealthyServices)
	}
}

// A node that is down or draining is the other half of "is the cluster ok".
func TestBuildClusterStatusNamesUnhealthyNodes(t *testing.T) {
	down := swarm.Node{
		ID:          "n1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateDown},
	}
	ready := swarm.Node{
		ID:          "n2",
		Description: swarm.NodeDescription{Hostname: "worker-2"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		Spec:        swarm.NodeSpec{Availability: swarm.NodeAvailabilityActive},
	}

	got := BuildClusterStatus(cache.ClusterSnapshot{}, nil, []swarm.Node{down, ready}, nil)

	if len(got.UnhealthyNodes) != 1 {
		t.Fatalf("unhealthy nodes = %d, want 1", len(got.UnhealthyNodes))
	}
	if got.UnhealthyNodes[0].Name != "worker-1" {
		t.Errorf("name = %q, want worker-1", got.UnhealthyNodes[0].Name)
	}
}

// Capacity in cores, both figures in the same unit, for the same reason
// cetacean://cluster was projected: reserved over total must be divisible.
func TestBuildClusterStatusReportsCapacityInCores(t *testing.T) {
	got := BuildClusterStatus(cache.ClusterSnapshot{
		TotalCPU:       12,
		ReservedCPU:    2_350_000_000,
		TotalMemory:    16 << 30,
		ReservedMemory: 2 << 30,
	}, nil, nil, nil)

	if got.Capacity.ReservedCPUCores != 2.35 {
		t.Errorf("ReservedCPUCores = %v, want 2.35", got.Capacity.ReservedCPUCores)
	}
	if got.Capacity.TotalCPUCores != 12 {
		t.Errorf("TotalCPUCores = %v, want 12", got.Capacity.TotalCPUCores)
	}
}
