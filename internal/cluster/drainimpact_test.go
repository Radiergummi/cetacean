package cluster

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func drainService(id, name string, replicas uint64, constraints ...string) swarm.Service {
	svc := swarm.Service{
		ID: id,
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	}
	if len(constraints) > 0 {
		svc.Spec.TaskTemplate.Placement = &swarm.Placement{Constraints: constraints}
	}

	return svc
}

func readyNode(id, hostname string, labels map[string]string) swarm.Node {
	return swarm.Node{
		ID: id,
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
			Annotations:  swarm.Annotations{Labels: labels},
		},
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

// A per-node replica cap is a hard scheduling filter, exactly as a constraint
// is: a service already at its cap on every remaining node has nowhere to go,
// and calling it movable would strand the replica as pending after the drain.
func TestDrainImpactStrandsAServiceAtItsPerNodeReplicaCap(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)
	second := readyNode("n2", "worker-2", nil)
	third := readyNode("n3", "worker-3", nil)

	web := drainService("svc1", "web", 3)
	web.Spec.TaskTemplate.Placement = &swarm.Placement{MaxReplicas: 1}

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, second, third},
		[]swarm.Task{
			liveTask("t1", "svc1", "n1"),
			liveTask("t2", "svc1", "n2"),
			liveTask("t3", "svc1", "n3"),
		},
		[]swarm.Service{web},
	)

	svc := findVertex(t, got, "svc1")
	if svc.State != "stranded" {
		t.Fatalf(
			"state = %q, want stranded: every candidate is at max_replicas_per_node",
			svc.State,
		)
	}
	if !strings.Contains(svc.Detail, "at most 1 replica") {
		t.Errorf("detail = %q, want the replica cap named", svc.Detail)
	}
	if len(got.Edges) != 0 {
		t.Errorf("edges = %+v, want none: no candidate has a free slot", got.Edges)
	}
}

// The cap only blocks a node that has actually reached it — and the tasks on
// the node being drained are the ones leaving, so they occupy nothing on the
// candidate. Here n2 holds 1 of at most 3, which leaves room for both replicas
// coming off n1.
func TestDrainImpactAllowsCandidatesUnderTheReplicaCap(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)
	spare := readyNode("n2", "worker-2", nil)

	web := drainService("svc1", "web", 3)
	web.Spec.TaskTemplate.Placement = &swarm.Placement{MaxReplicas: 3}

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, spare},
		[]swarm.Task{
			liveTask("t1", "svc1", "n1"),
			liveTask("t2", "svc1", "n1"),
			liveTask("t3", "svc1", "n2"),
		},
		[]swarm.Service{web},
	)

	svc := findVertex(t, got, "svc1")
	if svc.State != "movable" {
		t.Fatalf("state = %q, want movable: n2 runs 1 of at most 3", svc.State)
	}
	if len(got.Edges) != 1 || got.Edges[0].Target != "n2" {
		t.Errorf("edges = %+v, want svc1 -> n2", got.Edges)
	}
}

// A service whose image supports only one platform cannot move onto a node of
// another, which Swarm enforces as strictly as it enforces a constraint.
func TestDrainImpactStrandsAServicePinnedToAnotherPlatform(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)
	drained.Description.Platform = swarm.Platform{OS: "linux", Architecture: "arm64"}

	spare := readyNode("n2", "worker-2", nil)
	spare.Description.Platform = swarm.Platform{OS: "linux", Architecture: "amd64"}

	edge := drainService("svc1", "edge", 1)
	edge.Spec.TaskTemplate.Placement = &swarm.Placement{
		Platforms: []swarm.Platform{{OS: "linux", Architecture: "arm64"}},
	}

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, spare},
		[]swarm.Task{liveTask("t1", "svc1", "n1")},
		[]swarm.Service{edge},
	)

	svc := findVertex(t, got, "svc1")
	if svc.State != "stranded" {
		t.Fatalf("state = %q, want stranded: the only candidate is amd64", svc.State)
	}
	if !strings.Contains(svc.Detail, "linux/amd64") ||
		!strings.Contains(svc.Detail, "linux/arm64") {
		t.Errorf("detail = %q, want both the node's platform and the service's named", svc.Detail)
	}
}

// The same check must not strand a service the candidate can in fact run.
func TestDrainImpactAllowsAMatchingPlatform(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)
	drained.Description.Platform = swarm.Platform{OS: "linux", Architecture: "arm64"}

	spare := readyNode("n2", "worker-2", nil)
	spare.Description.Platform = swarm.Platform{OS: "linux", Architecture: "arm64"}

	edge := drainService("svc1", "edge", 1)
	edge.Spec.TaskTemplate.Placement = &swarm.Placement{
		Platforms: []swarm.Platform{
			{OS: "linux", Architecture: "amd64"},
			{OS: "linux", Architecture: "arm64"},
		},
	}

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, spare},
		[]swarm.Task{liveTask("t1", "svc1", "n1")},
		[]swarm.Service{edge},
	)

	if state := findVertex(t, got, "svc1").State; state != "movable" {
		t.Errorf("state = %q, want movable: the candidate matches one supported platform", state)
	}
}

// A free slot somewhere is not room for everything leaving. Two replicas on
// the drained node and one candidate holding one of a two-replica cap leaves
// exactly one slot — so one of the two would sit pending, and calling the
// service movable is the same wrong answer as naming a node that would refuse
// the task outright.
func TestDrainImpactStrandsWhenCandidatesCannotAbsorbEveryTask(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)
	spare := readyNode("n2", "worker-2", nil)

	web := drainService("svc1", "web", 3)
	web.Spec.TaskTemplate.Placement = &swarm.Placement{MaxReplicas: 2}

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, spare},
		[]swarm.Task{
			liveTask("t1", "svc1", "n1"),
			liveTask("t2", "svc1", "n1"),
			liveTask("t3", "svc1", "n2"),
		},
		[]swarm.Service{web},
	)

	svc := findVertex(t, got, "svc1")
	if svc.State != "stranded" {
		t.Fatalf("state = %q, want stranded: 2 tasks leaving, 1 slot free", svc.State)
	}
	if !strings.Contains(svc.Detail, "room for 1") {
		t.Errorf("detail = %q, want the actual free capacity named", svc.Detail)
	}
	if len(got.Edges) != 0 {
		t.Errorf("edges = %+v, want none: movability is claimed for the whole vertex", got.Edges)
	}
}

// And when the slots do add up, the service is movable as before.
func TestDrainImpactMovesWhenCandidatesHaveRoomForEveryTask(t *testing.T) {
	drained := readyNode("n1", "worker-1", nil)
	spare := readyNode("n2", "worker-2", nil)

	web := drainService("svc1", "web", 2)
	web.Spec.TaskTemplate.Placement = &swarm.Placement{MaxReplicas: 2}

	got := DrainImpactGraph(
		drained,
		[]swarm.Node{drained, spare},
		[]swarm.Task{
			liveTask("t1", "svc1", "n1"),
			liveTask("t2", "svc1", "n1"),
		},
		[]swarm.Service{web},
	)

	svc := findVertex(t, got, "svc1")
	if svc.State != "movable" {
		t.Fatalf("state = %q, want movable: 2 tasks leaving, 2 slots free", svc.State)
	}
	if len(got.Edges) != 1 || got.Edges[0].Target != "n2" {
		t.Errorf("edges = %+v, want svc1 -> n2", got.Edges)
	}
}
