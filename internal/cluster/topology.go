package cluster

import (
	"fmt"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// The two topology projections. A view name is part of the MCP tool's argument
// and result, so it is spelled once here rather than in each transport.
const (
	TopologyViewNetwork     = "network"
	TopologyViewPlacement   = "placement"
	TopologyViewDrainImpact = "drain-impact"
)

// TopologyGraph is a transport-neutral view of the cluster as a graph. Nodes
// and Edges are always non-nil, since a nil slice marshals to null and fails
// the advertised output schema, and both are sorted, so the same cluster state
// always produces the same bytes.
type TopologyGraph struct {
	View string `json:"view"`

	// Subject names the resource a view is *about*, where it is about one.
	// The network and placement views describe the whole cluster and leave it
	// empty; drain-impact describes one node, and the graph body — services on
	// one side, candidate nodes on the other — has nowhere to say which.
	Subject string `json:"subject,omitempty"`

	// Note is a caveat about the answer rather than any one vertex: a
	// drain-impact assessment concludes from the nodes it was given, so when
	// ACL filtering hid any, "stranded" is a statement about the caller's view
	// of the cluster and has to say so.
	Note string `json:"note,omitempty"`

	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges"`
}

// TopologyNode is one vertex: a service, an overlay network, or a cluster
// node. The fields are generic so a renderer can group and colour by Type and
// Group without knowing what a Swarm service is.
type TopologyNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`

	// Type is "service", "network" or "node".
	Type string `json:"type"`

	// Group clusters related vertices — the stack namespace, for a service.
	Group string `json:"group,omitempty"`

	// Detail is a single human-readable line: the image for a service, the
	// driver for a network, the role for a cluster node.
	Detail string `json:"detail,omitempty"`

	// State is the resource's condition, where it has one: the derived service
	// state, or a cluster node's status.
	State string `json:"state,omitempty"`
}

// TopologyEdge connects two vertices by ID.
type TopologyEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`

	// Label describes the relationship: network aliases, or how many of a
	// service's tasks a cluster node runs.
	Label string `json:"label,omitempty"`
}

// NetworkGraph projects services and the overlay networks they attach to as a
// bipartite graph, not the pairwise reachability graph REST /topology emits:
// that form is quadratic in the services sharing a network. Both slices must
// already be filtered to what the caller may read.
func NetworkGraph(services []swarm.Service, networks []network.Summary) TopologyGraph {
	overlays := OverlayNetworks(networks)

	nodes := make([]TopologyNode, 0, len(services)+len(overlays))
	edges := make([]TopologyEdge, 0, len(services))
	attached := make(map[string]struct{}, len(overlays))

	for _, svc := range services {
		nodes = append(nodes, serviceNode(svc, ""))

		for networkID, aliases := range ServiceAttachments(svc, overlays) {
			attached[networkID] = struct{}{}
			edges = append(edges, TopologyEdge{
				Source: svc.ID,
				Target: networkID,
				Label:  strings.Join(aliases, ", "),
			})
		}
	}

	// Only networks something actually attaches to become vertices: an unused
	// overlay is noise on a connectivity graph, and the REST projection omits
	// them for the same reason.
	for id := range attached {
		n := overlays[id]
		nodes = append(nodes, TopologyNode{
			ID:     id,
			Label:  n.Name,
			Type:   "network",
			Group:  n.Labels["com.docker.stack.namespace"],
			Detail: n.Driver,
			State:  n.Scope,
		})
	}

	return newGraph(TopologyViewNetwork, nodes, edges)
}

// OverlayNetworks indexes the overlay networks among networks by ID. Only
// overlays carry service-to-service connectivity, so every topology projection
// filters to them first.
func OverlayNetworks(networks []network.Summary) map[string]network.Summary {
	overlays := make(map[string]network.Summary, len(networks))

	for _, n := range networks {
		if n.Driver == "overlay" {
			overlays[n.ID] = n
		}
	}

	return overlays
}

// ServiceAttachments returns the overlay networks a service is on, mapped to
// its aliases there — the join every topology projection makes. Neither source
// alone is complete: a dnsrr service has no Endpoint.VirtualIPs, and the task
// template's networks say nothing about the aliases the scheduler resolved.
func ServiceAttachments(
	svc swarm.Service,
	overlays map[string]network.Summary,
) map[string][]string {
	attachments := make(map[string][]string)

	for _, vip := range svc.Endpoint.VirtualIPs {
		if _, ok := overlays[vip.NetworkID]; ok {
			attachments[vip.NetworkID] = nil
		}
	}

	for _, attachment := range svc.Spec.TaskTemplate.Networks {
		if _, ok := overlays[attachment.Target]; ok {
			attachments[attachment.Target] = attachment.Aliases
		}
	}

	return attachments
}

// PlacementGraph projects which cluster node runs which service as a bipartite
// graph, one edge per pair with tasks, labelled with how many are running.
// Tasks are the join rather than vertices of their own. Both slices must
// already be ACL-filtered; a task referencing anything outside them is dropped.
func PlacementGraph(
	clusterNodes []swarm.Node,
	tasks []swarm.Task,
	services []swarm.Service,
) TopologyGraph {
	visibleNodes := make(map[string]struct{}, len(clusterNodes))
	for _, n := range clusterNodes {
		visibleNodes[n.ID] = struct{}{}
	}

	visibleServices := make(map[string]swarm.Service, len(services))
	for _, svc := range services {
		visibleServices[svc.ID] = svc
	}

	// placement counts tasks per (cluster node, service) pair, and running
	// tasks per service, in a single pass over the task list.
	type pair struct{ nodeID, serviceID string }

	placed := make(map[pair][2]int)
	runningPerService := make(map[string]int)

	for _, task := range tasks {
		if !TaskIsLive(task) {
			continue
		}

		if _, ok := visibleNodes[task.NodeID]; !ok {
			continue
		}

		if _, ok := visibleServices[task.ServiceID]; !ok {
			continue
		}

		key := pair{task.NodeID, task.ServiceID}
		counts := placed[key]
		counts[0]++

		if task.Status.State == swarm.TaskStateRunning {
			counts[1]++
			runningPerService[task.ServiceID]++
		}

		placed[key] = counts
	}

	nodes := make([]TopologyNode, 0, len(clusterNodes)+len(services))

	for _, n := range clusterNodes {
		nodes = append(nodes, TopologyNode{
			ID:     n.ID,
			Label:  n.Description.Hostname,
			Type:   "node",
			Detail: string(n.Spec.Role),
			State:  deriveNodeState(n),
		})
	}

	// Every visible service is a vertex, including one with no task anywhere:
	// a service that cannot be scheduled is exactly what an operator opens this
	// view to find, and dropping it with its tasks would hide it.
	for _, svc := range services {
		nodes = append(nodes, serviceNode(svc, DeriveServiceState(svc, runningPerService[svc.ID])))
	}

	edges := make([]TopologyEdge, 0, len(placed))
	for key, counts := range placed {
		total, running := counts[0], counts[1]

		label := fmt.Sprintf("%d running", running)
		if running != total {
			label = fmt.Sprintf("%d/%d running", running, total)
		}

		edges = append(edges, TopologyEdge{
			Source: key.nodeID,
			Target: key.serviceID,
			Label:  label,
		})
	}

	return newGraph(TopologyViewPlacement, nodes, edges)
}

// TaskIsLive reports whether the orchestrator still intends this task to run.
// The rule lives in internal/cache, whose replica counters need it and cannot
// import this package; this is the name the projections here call it by.
func TaskIsLive(task swarm.Task) bool {
	return cache.TaskIsLive(task)
}

// serviceNode is the vertex a service contributes to either view, so the two
// cannot disagree about how a service is labelled, grouped or described.
func serviceNode(svc swarm.Service, state string) TopologyNode {
	var image string
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		image = StripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
	}

	return TopologyNode{
		ID:     svc.ID,
		Label:  svc.Spec.Name,
		Type:   "service",
		Group:  svc.Spec.Labels["com.docker.stack.namespace"],
		Detail: image,
		State:  state,
	}
}

// newGraph sorts a graph into its canonical order. Both halves are built by
// ranging over maps somewhere, and the result is marshalled into an MCP result
// a client may cache by ETag, so an arbitrary order would change the bytes for
// an unchanged cluster.
func newGraph(view string, nodes []TopologyNode, edges []TopologyEdge) TopologyGraph {
	slices.SortFunc(nodes, func(a, b TopologyNode) int {
		if c := strings.Compare(a.Type, b.Type); c != 0 {
			return c
		}
		if c := strings.Compare(a.Label, b.Label); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})

	slices.SortFunc(edges, func(a, b TopologyEdge) int {
		if c := strings.Compare(a.Source, b.Source); c != 0 {
			return c
		}
		return strings.Compare(a.Target, b.Target)
	})

	return TopologyGraph{View: view, Nodes: nodes, Edges: edges}
}

// StripImageDigest removes the content digest from an image reference, leaving
// the human-readable repository and tag. A pinned deployment otherwise renders
// as seventy-one characters of hex nobody reads.
func StripImageDigest(image string) string {
	if before, _, ok := strings.Cut(image, "@sha256:"); ok {
		return before
	}

	return image
}

// ReplicaCount is a replicated service's desired replica count, and 0 for a
// global service — whose desired count is whatever the scheduler decides the
// cluster's nodes can carry, and so is not a number the spec holds.
func ReplicaCount(svc swarm.Service) int {
	if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
		return int(*svc.Spec.Mode.Replicated.Replicas)
	}

	return 0
}
