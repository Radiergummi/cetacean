package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

func drainNode(id, hostname string) swarm.Node {
	return swarm.Node{
		ID:          id,
		Description: swarm.NodeDescription{Hostname: hostname},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
	}
}

func drainCache(constraints ...string) *cache.Cache {
	one := uint64(1)
	c := cache.New(nil)
	c.SetNode(drainNode("n1", "worker-1"))
	c.SetNode(drainNode("n2", "worker-2"))
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
			TaskTemplate: swarm.TaskSpec{
				Placement: &swarm.Placement{Constraints: constraints},
			},
		},
	})
	c.SetTask(swarm.Task{
		ID:           "t1",
		ServiceID:    "svc1",
		NodeID:       "n1",
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	return c
}

func getDrainImpact(
	t *testing.T,
	h *Handlers,
	id string,
	identity *auth.Identity,
) (int, cluster.TopologyGraph) {
	t.Helper()

	req := httptest.NewRequest("GET", "/nodes/"+id+"/drain-impact", nil)
	req.SetPathValue("id", id)
	if identity != nil {
		req = req.WithContext(auth.ContextWithIdentity(req.Context(), identity))
	}
	w := httptest.NewRecorder()
	h.HandleNodeDrainImpact(w, req)

	var graph cluster.TopologyGraph
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&graph); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}

	return w.Code, graph
}

func drainState(graph cluster.TopologyGraph, id string) string {
	for _, n := range graph.Nodes {
		if n.ID == id {
			return n.State
		}
	}

	return ""
}

// The dashboard's drain confirmation reads the same assessment MCP's
// drain-impact view gives, so the two cannot disagree about what moves.
func TestNodeDrainImpactNamesWhatMovesAndWhatCannot(t *testing.T) {
	h := newTestHandlers(t, withCache(drainCache()))

	code, graph := getDrainImpact(t, h, "n1", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if graph.View != cluster.TopologyViewDrainImpact || graph.Subject != "worker-1" {
		t.Errorf("view %q subject %q, want drain-impact about worker-1", graph.View, graph.Subject)
	}
	if got := drainState(graph, "svc1"); got != "movable" {
		t.Errorf("web = %q, want movable", got)
	}

	pinned := newTestHandlers(t, withCache(drainCache("node.hostname==worker-1")))
	if _, graph := getDrainImpact(t, pinned, "n1", nil); drainState(graph, "svc1") != "stranded" {
		t.Errorf("web pinned to worker-1 = %q, want stranded", drainState(graph, "svc1"))
	}
}

func TestNodeDrainImpactUnknownNode(t *testing.T) {
	h := newTestHandlers(t, withCache(drainCache()))

	if code, _ := getDrainImpact(t, h, "nope", nil); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

// A caller who cannot read every node is assessed against the ones they can,
// and told so, as MCP is.
func TestNodeDrainImpactSaysWhenGrantsHidNodes(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"node:worker-1", "service:*", "task:*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})
	h := newTestHandlers(t, withCache(drainCache()), withACL(e))

	code, graph := getDrainImpact(t, h, "n1", &auth.Identity{Subject: "user1"})
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !strings.Contains(graph.Note, "hidden") {
		t.Errorf("note = %q, want it to say nodes were hidden", graph.Note)
	}
	if got := drainState(graph, "svc1"); got != "stranded" {
		t.Errorf("web = %q, want stranded against the one readable node", got)
	}
}
