package mcp

import (
	"context"
	"fmt"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// toolGetTopology projects the cluster as a graph for the topology widget.
//
// The graph builders live in internal/cluster because a topology is a fact
// about the cluster, not about a transport — the REST handlers in internal/api
// read the same cache through the same rules, and a second implementation here
// is exactly how the two would drift.
//
// Every slice handed to a builder is ACL-filtered first: the builders drop any
// task whose node or service is missing from what they were given, so filtering
// the inputs is what keeps an unreadable resource out of the graph entirely
// rather than leaking it as a dangling edge.
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
// about one node and so needs it named.
//
// The node is resolved against the cache and read-checked before anything else
// happens, for the same reason get_metrics resolves its target first: the
// answer names the services running on it, so a caller must be permitted to
// read the node itself, not merely to see the graph.
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

	graph := cluster.DrainImpactGraph(
		node,
		visible,
		s.filterRawTasks(ctx, s.cache.ListTasksByNode(node.ID)),
		s.filterServices(ctx, s.cache.ListServices()),
	)

	// The candidates are the nodes the caller may read, so a service the
	// assessment calls stranded may in fact be placeable on one it cannot
	// see. The view exists to keep a drain from stranding work, and a
	// confident wrong answer is the failure it must not produce — so where the
	// node list was narrowed, the answer says what it was narrowed to.
	if hidden := len(all) - len(visible); hidden > 0 {
		graph.Note = fmt.Sprintf(
			"assessed against the %d node(s) you can read; %d more are hidden by "+
				"your grants, so a service reported stranded may be placeable on one of them",
			len(visible), hidden,
		)
	}

	return marshalResult(graph)
}
