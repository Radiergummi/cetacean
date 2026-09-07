package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// TestClusterResourceReportsCPUInOneNamedUnit pins the fix for the unit defect
// the live evaluation found: cache.ClusterSnapshot reports TotalCPU in whole
// cores and ReservedCPU in nanoCPUs, under two adjacent field names that carry
// no unit at all. The dashboard compensates by dividing one of them by 1e9
// before use (frontend/src/components/metrics/CapacitySection.tsx), but an MCP
// caller has no such secret to keep, and dividing reserved by total as spelled
// yields a number nine orders of magnitude wrong.
//
// The projection therefore names the unit in every numeric field, and puts both
// CPU figures in the same one, so "how much of the cluster is reserved?" is a
// division a reader can trust.
func TestClusterResourceReportsCPUInOneNamedUnit(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "node1",
		Description: swarm.NodeDescription{
			Hostname:  "worker-1",
			Resources: swarm.Resources{NanoCPUs: 12e9, MemoryBytes: 16 << 30},
		},
	})

	replicas := uint64(2)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
			TaskTemplate: swarm.TaskSpec{
				Resources: &swarm.ResourceRequirements{
					Reservations: &swarm.Resources{
						// 1.175 cores each, so two replicas reserve 2.35 —
						// a fractional total an integer field would destroy.
						NanoCPUs:    1_175_000_000,
						MemoryBytes: 512 << 20,
					},
				},
			},
		},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://cluster")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The unitless names must be gone: leaving them beside the new ones would
	// let a caller pick the wrong pair and be wrong in exactly the old way.
	for _, stale := range []string{"totalCPU", "reservedCPU", "totalMemory", "reservedMemory", "maxNodeCPU", "maxNodeMemory"} {
		if _, ok := got[stale]; ok {
			t.Errorf("field %q still present; every numeric field must name its unit", stale)
		}
	}

	assertNumber(t, got, "totalCPUCores", 12)
	assertNumber(t, got, "reservedCPUCores", 2.35)
	assertNumber(t, got, "maxNodeCPUCores", 12)
	assertNumber(t, got, "totalMemoryBytes", 16<<30)
	assertNumber(t, got, "reservedMemoryBytes", 2*(512<<20))
	assertNumber(t, got, "maxNodeMemoryBytes", 16<<30)

	// The whole point: reserved over total is a meaningful ratio.
	reserved, reservedOK := got["reservedCPUCores"].(float64)
	total, totalOK := got["totalCPUCores"].(float64)
	if reservedOK && totalOK && total > 0 && reserved/total > 1 {
		t.Errorf(
			"reserved/total CPU = %v, want a fraction; the two are not in the same unit",
			reserved/total,
		)
	}
}

// TestClusterResourceKeepsItsCounts guards the fields the projection only
// passes through, so narrowing it to the CPU figures cannot silently drop the
// rest of the overview.
func TestClusterResourceKeepsItsCounts(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://cluster")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, want := range []string{"nodeCount", "serviceCount", "taskCount", "stackCount", "tasksByState", "nodesReady", "nodesDown", "nodesDraining", "servicesConverged", "servicesDegraded", "lastSync"} {
		if _, ok := got[want]; !ok {
			t.Errorf("field %q missing from the cluster overview", want)
		}
	}
}

func assertNumber(t *testing.T, got map[string]any, field string, want float64) {
	t.Helper()

	raw, ok := got[field]
	if !ok {
		t.Errorf("field %q missing", field)
		return
	}

	n, ok := raw.(float64)
	if !ok {
		t.Errorf("field %q = %T, want a number", field, raw)
		return
	}

	if n != want {
		t.Errorf("%s = %v, want %v", field, n, want)
	}
}
