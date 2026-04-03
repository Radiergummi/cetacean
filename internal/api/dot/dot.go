// Package dot renders a jgf.Graph as Graphviz DOT format.
package dot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/jgf"
)

// Render serializes g as an undirected Graphviz DOT graph.
func Render(g jgf.Graph) ([]byte, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "graph %q {\n", g.Label)

	// Collect nodes that belong to a stack hyperedge.
	stackNodes := make(map[string]string) // nodeURN → stackName

	// Sort stacks by name for deterministic output.
	type stackEntry struct {
		name  string
		nodes []string
	}

	var stacks []stackEntry

	for _, he := range g.Hyperedges {
		kind, _ := he.Metadata["kind"].(string)
		if kind != "stack" {
			continue
		}

		name, _ := he.Metadata["name"].(string)
		if name == "" {
			continue
		}

		members := make([]string, len(he.Nodes))
		copy(members, he.Nodes)
		sort.Strings(members)

		stacks = append(stacks, stackEntry{name: name, nodes: members})

		for _, urn := range he.Nodes {
			stackNodes[urn] = name
		}
	}

	sort.Slice(stacks, func(i, j int) bool {
		return stacks[i].name < stacks[j].name
	})

	// Emit stack subgraphs.
	for _, stack := range stacks {
		fmt.Fprintf(&b, "\tsubgraph %q {\n", "cluster_"+stack.name)
		fmt.Fprintf(&b, "\t\tlabel=%q;\n", stack.name)

		for _, urn := range stack.nodes {
			node, ok := g.Nodes[urn]
			if !ok {
				continue
			}

			fmt.Fprintf(&b, "\t\t%s;\n", nodeStatement(urn, node))
		}

		fmt.Fprintf(&b, "\t}\n")
	}

	// Emit top-level nodes (not in any stack), sorted by URN.
	var topLevel []string

	for urn := range g.Nodes {
		if _, inStack := stackNodes[urn]; !inStack {
			topLevel = append(topLevel, urn)
		}
	}

	sort.Strings(topLevel)

	for _, urn := range topLevel {
		fmt.Fprintf(&b, "\t%s;\n", nodeStatement(urn, g.Nodes[urn]))
	}

	// Emit edges.
	for _, edge := range g.Edges {
		edgeLabel := extractNetworkNames(edge.Metadata)
		fmt.Fprintf(&b, "\t%q -- %q [label=%q];\n", edge.Source, edge.Target, edgeLabel)
	}

	fmt.Fprintf(&b, "}\n")

	return []byte(b.String()), nil
}

// nodeStatement builds the DOT node declaration for a single node.
func nodeStatement(urn string, node jgf.Node) string {
	replicas, _ := node.Metadata["replicas"].(int)
	image, _ := node.Metadata["image"].(string)
	mode, _ := node.Metadata["mode"].(string)

	return fmt.Sprintf("%q [label=%q replicas=%d image=%q mode=%q]",
		urn, node.Label, replicas, image, mode)
}

// extractNetworkNames collects network names from edge metadata.
func extractNetworkNames(meta jgf.Metadata) string {
	networks, _ := meta["networks"].([]any)
	names := make([]string, 0, len(networks))

	for _, entry := range networks {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		name, _ := m["name"].(string)
		if name != "" {
			names = append(names, name)
		}
	}

	return strings.Join(names, ",")
}
