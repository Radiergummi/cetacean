package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// A count missing from the map is not a count of zero: a global service with
// nothing running reads pending, as it did before counts were known at all.
func TestAMissingEligibleCountReadsPending(t *testing.T) {
	svc := swarm.Service{
		ID: "agent",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "agent"},
			Mode:        swarm.ServiceMode{Global: &swarm.GlobalService{}},
		},
	}
	services := []swarm.Service{svc}

	if got := RowsForServices(services, nil, nil, nil, nil)[0].State; got != "pending" {
		t.Errorf("RowsForServices = %q, want pending", got)
	}
	if got := PlacementGraph(nil, nil, services, nil, nil).Nodes[0].State; got != "pending" {
		t.Errorf("PlacementGraph = %q, want pending", got)
	}
}
