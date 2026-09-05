package cluster

import (
	"fmt"

	"github.com/docker/docker/api/types/swarm"
)

// The condition of a service on the node being drained.
const (
	// drainStateMovable means at least one remaining node satisfies the
	// service's placement constraints.
	drainStateMovable = "movable"

	// drainStateStranded means none does. This is the answer the view exists
	// to give, and the vertex carries the blocking constraint as its detail.
	drainStateStranded = "stranded"

	// drainStateGlobal means the service runs one task per node, so a drain
	// does not relocate its task — it simply stops running there. Neither
	// "movable" nor "stranded" is true of it, and saying either would mislead.
	drainStateGlobal = "global"
)

// DrainImpactGraph answers "if I drain this node, what moves — and what
// cannot?" as a bipartite graph of the affected services against the nodes
// that could take them.
//
// This is steps 3 and 4 of the drain_node prompt done server-side. The prompt
// currently instructs the model to page a task listing, list the nodes, and
// compare each service's placement constraints against them by eye — a join
// the model pays for in its context and can get wrong, on a question where
// being wrong means draining a node and stranding the work.
//
// An affected service with no edge is stranded, and Detail carries the
// constraint that blocked it: a state without its cause is exactly what this
// view must not return. Capacity is deliberately not considered — placement
// constraints are what Swarm enforces exactly, while spare reservations after
// a drain are a moving target, and cluster capacity is already reported by the
// cluster status read.
//
// clusterNodes, tasks and services must already be filtered to what the caller
// may read; a service whose tasks reference nothing visible simply does not
// appear, the same rule the other two views follow.
func DrainImpactGraph(
	target swarm.Node,
	clusterNodes []swarm.Node,
	tasks []swarm.Task,
	services []swarm.Service,
) TopologyGraph {
	servicesByID := make(map[string]swarm.Service, len(services))
	for _, svc := range services {
		servicesByID[svc.ID] = svc
	}

	// Count the live tasks the drained node carries per service: the services
	// with at least one are the work that has to land somewhere else.
	onTarget := make(map[string]int)

	for _, task := range tasks {
		if task.NodeID != target.ID || !taskIsLive(task) {
			continue
		}

		if _, ok := servicesByID[task.ServiceID]; !ok {
			continue
		}

		onTarget[task.ServiceID]++
	}

	// Candidates are every *other* node that can still accept work. A node
	// that is down cannot take it, and one already draining or paused has been
	// told not to — so neither is somewhere the work would actually land.
	candidates := make([]swarm.Node, 0, len(clusterNodes))

	for _, n := range clusterNodes {
		if n.ID == target.ID {
			continue
		}

		if n.Status.State != swarm.NodeStateReady ||
			n.Spec.Availability != swarm.NodeAvailabilityActive {
			continue
		}

		candidates = append(candidates, n)
	}

	nodes := make([]TopologyNode, 0, len(onTarget)+len(candidates))
	edges := make([]TopologyEdge, 0, len(onTarget))

	for _, n := range candidates {
		nodes = append(nodes, TopologyNode{
			ID:     n.ID,
			Label:  n.Description.Hostname,
			Type:   "node",
			Detail: string(n.Spec.Role),
			State:  DeriveNodeState(n),
		})
	}

	for serviceID, running := range onTarget {
		svc := servicesByID[serviceID]

		state, detail, accepted := placementFor(svc, running, candidates)

		vertex := serviceNode(svc, state)
		vertex.Detail = detail
		nodes = append(nodes, vertex)

		for _, nodeID := range accepted {
			edges = append(edges, TopologyEdge{
				Source: serviceID,
				Target: nodeID,
				Label:  "can host",
			})
		}
	}

	graph := newGraph(TopologyViewDrainImpact, nodes, edges)
	graph.Subject = target.Description.Hostname

	return graph
}

// placementFor decides where one affected service could go, returning its
// state, the detail line for its vertex, and the candidate node IDs to draw an
// edge to. running is how many of its tasks the drained node carries — the
// amount of work actually in question.
//
// The blocking constraint reported for a stranded service is the one that
// failed on the *last* candidate examined rather than a summary of all of
// them: every candidate failing on the same constraint is the usual case, and
// naming one constraint a caller can act on beats listing the same string once
// per node.
func placementFor(
	svc swarm.Service,
	running int,
	candidates []swarm.Node,
) (state, detail string, accepted []string) {
	if svc.Spec.Mode.Global != nil {
		return drainStateGlobal, "one task per node; draining stops it here rather than moving it", nil
	}

	var constraints []string
	if p := svc.Spec.TaskTemplate.Placement; p != nil {
		constraints = p.Constraints
	}

	var blocked string

	for _, n := range candidates {
		ok, reason := NodeSatisfies(n, constraints)
		if !ok {
			blocked = reason

			continue
		}

		accepted = append(accepted, n.ID)
	}

	if len(accepted) > 0 {
		return drainStateMovable, fmt.Sprintf(
			"%d task(s) here; %d node(s) can take them",
			running, len(accepted),
		), accepted
	}

	if blocked != "" {
		return drainStateStranded, blocked, nil
	}

	return drainStateStranded, "no other node is ready and active", nil
}
