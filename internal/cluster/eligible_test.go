package cluster

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// A global service with nothing running reads pending while a node could take
// it and, like one scaled to zero, running when none can. Every view that
// derives a service's state must agree, or the overview and the page disagree.
func TestAGlobalServiceReadsTheSameStateEverywhere(t *testing.T) {
	for _, tc := range []struct {
		availability swarm.NodeAvailability
		want         string
	}{
		{swarm.NodeAvailabilityActive, "pending"},
		{swarm.NodeAvailabilityDrain, "running"},
		{swarm.NodeAvailabilityPause, "running"},
	} {
		t.Run(string(tc.availability), func(t *testing.T) {
			c := cache.New(nil)
			c.SetNode(swarm.Node{
				ID:     "n1",
				Status: swarm.NodeStatus{State: swarm.NodeStateReady},
				Spec:   swarm.NodeSpec{Availability: tc.availability},
			})
			svc := swarm.Service{
				ID: "agent",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{
						Name:   "agent",
						Labels: map[string]string{StackNamespaceLabel: "ops"},
					},
					Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}},
				},
			}
			c.SetService(svc)

			services := []swarm.Service{svc}
			eligible := c.EligibleNodeCounts(services...)
			snap := c.Snapshot()

			got := map[string]string{
				"DeriveServiceState": DeriveServiceState(
					svc, 0, 0, EligibleIn(eligible, svc.ID), false,
				),
				"RowsForServices": RowsForServices(services, nil, nil, eligible, nil)[0].State,
				"ServiceDigest": ServiceDigest(
					svc, nil, nil, nil, false, EligibleIn(eligible, svc.ID), nil,
				).State,
				"StackDigest": StackDigest(
					cache.StackDetail{Name: "ops", Services: services}, nil, nil, eligible,
				).State,
				"PlacementGraph": PlacementGraph(nil, nil, services, nil, eligible).Nodes[0].State,
				"Search": Search(context.Background(), c, "agent", 0, nil).
					Hits["services"][0].State,
			}

			for view, state := range got {
				if state != tc.want {
					t.Errorf("%s = %q, want %q", view, state, tc.want)
				}
			}

			converged, degraded := CountServiceStates(snap, services, eligible)
			if (tc.want == "running") != (converged == 1 && degraded == 0) {
				t.Errorf("converged=%d degraded=%d for a service reading %q",
					converged, degraded, tc.want)
			}

			unhealthy := len(
				BuildClusterStatus(snap, services, nil, nil, eligible).UnhealthyServices,
			)
			if (tc.want == "pending") != (unhealthy == 1) {
				t.Errorf("unhealthy services = %d for a service reading %q", unhealthy, tc.want)
			}
		})
	}
}

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
