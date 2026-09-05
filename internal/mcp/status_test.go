package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// The whole surface's landing question, end to end: one call, and the broken
// thing is named rather than counted.
func TestGetClusterStatusNamesTheBrokenService(t *testing.T) {
	c := cache.New(nil)
	one := uint64(1)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "demo_stuck"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.toolGetClusterStatus(
		context.Background(),
		toolRequest(t, "get_cluster_status", nil),
	)
	if err != nil {
		t.Fatalf("toolGetClusterStatus: %v", err)
	}

	var got cluster.ClusterStatus
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Healthy {
		t.Error("Healthy = true with a service that has no running replica")
	}
	if len(got.UnhealthyServices) != 1 || got.UnhealthyServices[0].Name != "demo_stuck" {
		t.Errorf("unhealthyServices = %+v, want one named demo_stuck", got.UnhealthyServices)
	}
}

// Empty arrays, never null — the rule every schema-advertising type here
// follows, and the one a strict client trips over.
func TestGetClusterStatusMarshalsEmptyArrays(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	body, err := srv.toolGetClusterStatus(
		context.Background(),
		toolRequest(t, "get_cluster_status", nil),
	)
	if err != nil {
		t.Fatalf("toolGetClusterStatus: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, field := range []string{"unhealthyServices", "unhealthyNodes", "rollouts"} {
		if raw[field] == nil {
			t.Errorf("%s marshalled as null, want []", field)
		}
	}
}
