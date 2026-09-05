package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func drainService(id, name string, replicas uint64, constraints ...string) swarm.Service {
	svc := swarm.Service{
		ID: id,
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
		},
	}
	if len(constraints) > 0 {
		svc.Spec.TaskTemplate.Placement = &swarm.Placement{Constraints: constraints}
	}

	return svc
}

func readyNode(id, hostname string, labels map[string]string) swarm.Node {
	return swarm.Node{
		ID:          id,
		Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker, Availability: swarm.NodeAvailabilityActive, Annotations: swarm.Annotations{Labels: labels}},
		Description: swarm.NodeDescription{Hostname: hostname},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	}
}

func liveTask(id, serviceID, nodeID string) swarm.Task {
	return swarm.Task{
		ID:           id,
		ServiceID:    serviceID,
		NodeID:       nodeID,
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	}
}

// The question the view exists for: a service on the drained node that another
// node can take is movable, and the node that can take it is the edge.
func TestDrainImpactPairsAMovableServiceWithItsCandidates(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)
	spare := readyNode("n2", "worker-2", nil)
	web := drainService("svc1", "web", 1)

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, spare},
		[]swarm.Task{liveTask("t1", "svc1", "n1")},
		[]swarm.Service{web},
	)

	if got.View != TopologyViewDrainImpact {
		t.Errorf("view = %q, want %q", got.View, TopologyViewDrainImpact)
	}
	if got.Subject != "worker-1" {
		t.Errorf("subject = %q, want the drained node's hostname", got.Subject)
	}

	svc := findVertex(t, got, "svc1")
	if svc.State != "movable" {
		t.Errorf("state = %q, want movable", svc.State)
	}

	if len(got.Edges) != 1 || got.Edges[0].Source != "svc1" || got.Edges[0].Target != "n2" {
		t.Errorf("edges = %+v, want svc1 -> n2", got.Edges)
	}
}

// The answer that matters: a service no remaining node can satisfy is stranded,
// and the blocking constraint is on the vertex. A state without its cause is
// exactly what this view exists not to return.
func TestDrainImpactStrandsAServiceNoNodeSatisfies(t *testing.T) {
	drained := readyNode("n1", "worker-1", map[string]string{"gpu": "true"})
	spare := readyNode("n2", "worker-2", nil)
	trainer := drainService("svc1", "trainer", 1, "node.labels.gpu==true")

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, spare},
		[]swarm.Task{liveTask("t1", "svc1", "n1")},
		[]swarm.Service{trainer},
	)

	svc := findVertex(t, got, "svc1")
	if svc.State != "stranded" {
		t.Fatalf("state = %q, want stranded", svc.State)
	}
	if svc.Detail != "node.labels.gpu==true" {
		t.Errorf("detail = %q, want the blocking constraint", svc.Detail)
	}
	if len(got.Edges) != 0 {
		t.Errorf("edges = %+v, want none for a stranded service", got.Edges)
	}
}

// The drained node is never its own candidate — it is the thing being emptied.
func TestDrainImpactExcludesTheDrainedNode(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)
	web := drainService("svc1", "web", 1)

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained},
		[]swarm.Task{liveTask("t1", "svc1", "n1")},
		[]swarm.Service{web},
	)

	for _, n := range got.Nodes {
		if n.ID == "n1" {
			t.Error("the drained node appears as a candidate")
		}
	}

	svc := findVertex(t, got, "svc1")
	if svc.State != "stranded" {
		t.Errorf("state = %q, want stranded: it is the only node", svc.State)
	}
}

// A node that is down or already draining cannot take the work either.
func TestDrainImpactIgnoresUnavailableNodes(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)

	down := readyNode("n2", "worker-2", nil)
	down.Status.State = swarm.NodeStateDown

	paused := readyNode("n3", "worker-3", nil)
	paused.Spec.Availability = swarm.NodeAvailabilityDrain

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, down, paused},
		[]swarm.Task{liveTask("t1", "svc1", "n1")},
		[]swarm.Service{drainService("svc1", "web", 1)},
	)

	if len(got.Edges) != 0 {
		t.Errorf("edges = %+v, want none: neither remaining node can accept work", got.Edges)
	}
	if findVertex(t, got, "svc1").State != "stranded" {
		t.Error("want stranded when every other node is down or draining")
	}
}

// A global service is not relocated by a drain — it simply stops running
// there. Reporting it movable would be a lie, and reporting it stranded would
// invent a blocker that does not exist.
func TestDrainImpactReportsGlobalServicesSeparately(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)
	spare := readyNode("n2", "worker-2", nil)

	agent := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "monitoring_agent"},
			Mode:        swarm.ServiceMode{Global: &swarm.GlobalService{}},
		},
	}

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, spare},
		[]swarm.Task{liveTask("t1", "svc1", "n1")},
		[]swarm.Service{agent},
	)

	svc := findVertex(t, got, "svc1")
	if svc.State != "global" {
		t.Errorf("state = %q, want global", svc.State)
	}
	if len(got.Edges) != 0 {
		t.Errorf("edges = %+v, want none: a global task is not relocated", got.Edges)
	}
}

// Only services actually running on the drained node are affected.
func TestDrainImpactIgnoresServicesElsewhere(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)
	spare := readyNode("n2", "worker-2", nil)

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, spare},
		[]swarm.Task{liveTask("t1", "svc1", "n1"), liveTask("t2", "svc2", "n2")},
		[]swarm.Service{drainService("svc1", "web", 1), drainService("svc2", "api", 1)},
	)

	for _, n := range got.Nodes {
		if n.ID == "svc2" {
			t.Error("a service running elsewhere was reported as affected")
		}
	}
}

// Nothing on the node is an empty graph, not a nil one.
func TestDrainImpactOnAnEmptyNodeMarshalsArrays(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)

	got := DrainImpactGraph(drained, []swarm.Node{drained}, nil, nil)

	if got.Nodes == nil || got.Edges == nil {
		t.Errorf("nil slices: %+v", got)
	}
}

func findVertex(t *testing.T, graph TopologyGraph, id string) TopologyNode {
	t.Helper()

	for _, n := range graph.Nodes {
		if n.ID == id {
			return n
		}
	}

	t.Fatalf("no vertex %q in %+v", id, graph.Nodes)

	return TopologyNode{}
}
