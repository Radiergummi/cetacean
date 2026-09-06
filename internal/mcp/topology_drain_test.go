package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

func drainCall(t *testing.T, srv *Server, node string) cluster.TopologyGraph {
	t.Helper()

	body, err := srv.toolGetTopology(
		context.Background(),
		toolRequest(t, "get_topology", map[string]any{
			"view": "drain-impact",
			"node": node,
		}),
	)
	if err != nil {
		t.Fatalf("toolGetTopology: %v", err)
	}

	var got cluster.TopologyGraph
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	return got
}

func drainTestCache(t *testing.T) *cache.Cache {
	t.Helper()

	c := cache.New(nil)
	one := uint64(1)

	for _, n := range []struct{ id, host string }{{"n1", "worker-1"}, {"n2", "worker-2"}} {
		c.SetNode(swarm.Node{
			ID: n.id,
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleWorker,
				Availability: swarm.NodeAvailabilityActive,
			},
			Description: swarm.NodeDescription{Hostname: n.host},
			Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		})
	}

	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
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

// The node is named the way every other tool takes one — by name or ID — and
// the graph says which node it is about.
func TestDrainImpactViewResolvesTheNodeByName(t *testing.T) {
	srv := newResourceTestServer(t, drainTestCache(t))

	got := drainCall(t, srv, "worker-1")

	if got.View != "drain-impact" {
		t.Errorf("view = %q", got.View)
	}
	if got.Subject != "worker-1" {
		t.Errorf("subject = %q, want worker-1", got.Subject)
	}
	if len(got.Edges) != 1 || got.Edges[0].Target != "n2" {
		t.Errorf("edges = %+v, want the service paired with worker-2", got.Edges)
	}
}

// The view is about one node, so omitting it is an error rather than a graph
// of the whole cluster — which is what the other two views already are.
func TestDrainImpactViewRequiresANode(t *testing.T) {
	srv := newResourceTestServer(t, drainTestCache(t))

	_, err := srv.toolGetTopology(
		context.Background(),
		toolRequest(t, "get_topology", map[string]any{
			"view": "drain-impact",
		}),
	)
	if err == nil {
		t.Fatal("expected an error naming the required node argument")
	}
}

func TestDrainImpactViewRejectsAnUnknownNode(t *testing.T) {
	srv := newResourceTestServer(t, drainTestCache(t))

	_, err := srv.toolGetTopology(
		context.Background(),
		toolRequest(t, "get_topology", map[string]any{
			"view": "drain-impact",
			"node": "nosuch",
		}),
	)
	if err == nil {
		t.Fatal("expected an error for an unknown node")
	}
}

// The existing views must keep ignoring the new argument.
func TestExistingViewsIgnoreTheNodeArgument(t *testing.T) {
	srv := newResourceTestServer(t, drainTestCache(t))

	for _, view := range []string{"network", "placement"} {
		body, err := srv.toolGetTopology(
			context.Background(),
			toolRequest(t, "get_topology", map[string]any{
				"view": view,
			}),
		)
		if err != nil {
			t.Fatalf("%s: %v", view, err)
		}

		var got cluster.TopologyGraph
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("%s unmarshal: %v", view, err)
		}

		if got.Subject != "" {
			t.Errorf("%s: subject = %q, want empty for a cluster-wide view", view, got.Subject)
		}
	}
}

// The candidates are the nodes the caller may read, so a service the
// assessment calls stranded may in fact be placeable on one they cannot see.
// "no other node is ready and active" is a confident wrong answer on the one
// question this view exists to get right, so the graph says what it was
// assessed against.
func TestDrainImpactSaysWhenGrantsNarrowedTheCandidates(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("node:worker-1", "service:*", "task:*"))

	srv := newResourceTestServer(t, drainTestCache(t), func(o *Options) { o.ACL = evaluator })

	body, err := srv.toolGetTopology(
		ctxWithIdentity(),
		toolRequest(t, "get_topology", map[string]any{
			"view": "drain-impact",
			"node": "worker-1",
		}),
	)
	if err != nil {
		t.Fatalf("toolGetTopology: %v", err)
	}

	var got cluster.TopologyGraph
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Note == "" {
		t.Fatal("no note, so a stranded verdict reads as a statement about the cluster")
	}
	if !strings.Contains(got.Note, "hidden") {
		t.Errorf("note = %q, want it to say nodes were hidden", got.Note)
	}
}

// With nothing hidden the note would be noise on every call, and a reader who
// sees it once has to be able to trust it means something.
func TestDrainImpactHasNoNoteWhenEveryNodeIsVisible(t *testing.T) {
	srv := newResourceTestServer(t, drainTestCache(t))

	if got := drainCall(t, srv, "worker-1"); got.Note != "" {
		t.Errorf("note = %q, want none when no node was hidden", got.Note)
	}
}
