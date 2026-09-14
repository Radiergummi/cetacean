package mcp

import (
	"context"
	"fmt"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// toolGetTopology projects the cluster as a graph for the topology widget. The
// builders live in internal/cluster because a topology is a fact about the
// cluster, not a transport. Every slice is ACL-filtered first: a builder drops
// a task whose node or service is missing, so nothing leaks as a dangling edge.
func (s *Server) toolGetTopology(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	view := req.GetString("view", cluster.TopologyViewNetwork)

	switch view {
	case cluster.TopologyViewNetwork:
		return marshalResult(cluster.NetworkGraph(
			s.filterServices(ctx, s.cache.ListServices()),
			s.filterNetworks(ctx, s.cache.ListNetworks()),
		))

	case cluster.TopologyViewPlacement:
		return marshalResult(cluster.PlacementGraph(
			s.filterNodes(ctx, s.cache.ListNodes()),
			s.cache.ListTasks(),
			s.filterServices(ctx, s.cache.ListServices()),
		))

	case cluster.TopologyViewDrainImpact:
		return s.drainImpact(ctx, req)

	default:
		return "", fmt.Errorf(
			"unknown view %q; expected %q, %q or %q",
			view,
			cluster.TopologyViewNetwork,
			cluster.TopologyViewPlacement,
			cluster.TopologyViewDrainImpact,
		)
	}
}

// drainImpact answers the drain-impact view, which unlike the other two is
// about one node and so needs it named. The node is resolved and read-checked
// before anything else: the answer names the services running on it, so the
// caller must be permitted to read the node itself.
func (s *Server) drainImpact(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	identifier := req.GetString("node", "")
	if identifier == "" {
		return "", fmt.Errorf("node: required for the %q view", cluster.TopologyViewDrainImpact)
	}

	node, found, err := s.cache.ResolveNode(identifier)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("no such node %q", identifier)
	}

	if err := s.checkRead(ctx, "node", nodeACLName(node)); err != nil {
		return "", err
	}

	all := s.cache.ListNodes()
	visible := s.filterNodes(ctx, all)

	// The cluster's tasks rather than the target node's: DrainImpactGraph
	// measures a service's per-node replica cap against what each *candidate*
	// already runs, and the node's own index reports every candidate empty.
	// A task inherits its service's grant, so ACL filtering keeps it exact.
	graph := cluster.DrainImpactGraph(
		node,
		visible,
		s.filterRawTasks(ctx, s.cache.ListTasks()),
		s.filterServices(ctx, s.cache.ListServices()),
	)

	// The candidates are the nodes the caller may read, so a service called
	// stranded may in fact be placeable on one it cannot see. The view exists
	// to keep a drain from stranding work, so where the node list was
	// narrowed, the answer says so.
	if hidden := len(all) - len(visible); hidden > 0 {
		graph.Note = fmt.Sprintf(
			"assessed against the %d node(s) you can read; %d more are hidden by "+
				"your grants, so a service reported stranded may be placeable on one of them",
			len(visible), hidden,
		)
	}

	return marshalResult(graph)
}
