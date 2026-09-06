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
// An affected service with no edge is stranded, and Detail carries the filter
// that blocked it: a state without its cause is exactly what this view must
// not return. Which filters those are is nodeCanHost's answer — the three
// Swarm enforces exactly. Capacity is deliberately not among them: spare
// reservations after a drain are a moving target, and cluster capacity is
// already reported by the cluster status read.
//
// tasks must cover the cluster, not just the target: the per-node replica cap
// is measured against the tasks a *candidate* already carries, so a node-scoped
// slice would report every candidate empty and call a capped service movable
// onto nodes that are full.
//
// clusterNodes, tasks and services must already be filtered to what the caller
// may read; a service whose tasks reference nothing visible simply does not
// appear, the same rule the other two views follow. That cuts both ways here,
// unlike in the other two: the candidates are the nodes the caller may read,
// so a service this reports as stranded may be placeable on one they cannot
// see. A caller that narrowed the list is expected to say so in the graph's
// Note — internal/mcp's drainImpact does.
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

	// And, per service, what every *other* node already carries — the replica
	// budget a service with MaxReplicas has left there. Tasks on the target are
	// deliberately absent: they are the ones leaving, so they occupy nothing.
	placed := make(map[string]map[string]int)

	for _, task := range tasks {
		if !TaskIsLive(task) {
			continue
		}

		if _, ok := servicesByID[task.ServiceID]; !ok {
			continue
		}

		if task.NodeID == target.ID {
			onTarget[task.ServiceID]++

			continue
		}

		byNode := placed[task.ServiceID]
		if byNode == nil {
			byNode = make(map[string]int)
			placed[task.ServiceID] = byNode
		}

		byNode[task.NodeID]++
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
			State:  deriveNodeState(n),
		})
	}

	for serviceID, running := range onTarget {
		svc := servicesByID[serviceID]

		state, detail, accepted := placementFor(svc, running, candidates, placed[serviceID])

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
// The blocking reason reported for a stranded service is the one that failed
// on the *last* candidate examined rather than a summary of all of them: every
// candidate failing on the same filter is the usual case, and naming one a
// caller can act on beats listing the same string once per node.
//
// A service whose work only *partly* fits elsewhere is reported stranded with
// no edges, not movable. "Movable" claims the whole vertex can leave — the
// detail says "N task(s) here; M node(s) can take them" — and an operator
// draining on that basis is left with pending replicas, so the state a caller
// acts on has to be the pessimistic one. The detail then says how much room
// there actually is.
//
// placed maps a candidate node ID to how many of this service's live tasks it
// already runs, which is what nodeCanHost measures the per-node replica cap
// against. A nil map is a service with no tasks anywhere else.
func placementFor(
	svc swarm.Service,
	running int,
	candidates []swarm.Node,
	placed map[string]int,
) (state, detail string, accepted []string) {
	if svc.Spec.Mode.Global != nil {
		return drainStateGlobal, "one task per node; draining stops it here rather than moving it", nil
	}

	placement := svc.Spec.TaskTemplate.Placement

	var blocked string

	for _, n := range candidates {
		ok, reason := nodeCanHost(n, placement, placed[n.ID])
		if !ok {
			blocked = reason

			continue
		}

		accepted = append(accepted, n.ID)
	}

	if len(accepted) > 0 {
		// A candidate with one free slot is not the same as room for all the
		// work. Under a per-node cap the slots have to add up across every
		// candidate, or the surplus replicas sit pending after the drain —
		// which is the same wrong answer as naming a node that would refuse
		// the task outright, arrived at one replica later.
		if placement != nil && placement.MaxReplicas > 0 {
			var free uint64

			for _, id := range accepted {
				if held := uint64(placed[id]); held < placement.MaxReplicas {
					free += placement.MaxReplicas - held
				}
			}

			if free < uint64(running) {
				return drainStateStranded, fmt.Sprintf(
					"%d task(s) here, but the %d candidate node(s) have room for %d "+
						"between them: at most %d replica(s) per node",
					running, len(accepted), free, placement.MaxReplicas,
				), nil
			}
		}

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
