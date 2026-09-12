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
// cannot?" as a bipartite graph of affected services against the nodes that
// could take them. A service with no edge is stranded, and Detail names the
// filter nodeCanHost blocked it on. tasks must cover the cluster, not just the
// target: the per-node cap is measured against what each candidate carries.
// Every slice must be ACL-filtered, so the candidates are only readable nodes.
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

// placementFor decides where one affected service could go: its state, its
// detail line, and the candidates to draw an edge to. A stranded service
// reports why the *last* candidate failed, since they usually all fail alike.
// Work that only partly fits elsewhere is stranded rather than movable, or an
// operator is left with pending replicas. placed is each candidate's existing
// live-task count for this service; nil means it has tasks nowhere else.
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
		// A candidate with one free slot is not room for all the work: under a
		// per-node cap the slots have to add up across every candidate, or the
		// surplus replicas sit pending after the drain.
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
