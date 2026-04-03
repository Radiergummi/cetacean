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

	fmt.Fprintf(&b, "graph %s {\n", dotQuote(g.Label))

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
		fmt.Fprintf(&b, "\tsubgraph %s {\n", dotQuote("cluster_"+stack.name))
		fmt.Fprintf(&b, "\t\tlabel=%s;\n", dotQuote(stack.name))

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
		fmt.Fprintf(
			&b,
			"\t%s -- %s [label=%s];\n",
			dotQuote(edge.Source),
			dotQuote(edge.Target),
			dotQuote(edgeLabel),
		)
	}

	fmt.Fprintf(&b, "}\n")

	return []byte(b.String()), nil
}

// nodeStatement builds the DOT node declaration for a single node.
func nodeStatement(urn string, node jgf.Node) string {
	attrs := []string{
		"label=" + dotQuote(node.Label),
	}

	if v, ok := node.Metadata["replicas"]; ok {
		attrs = append(attrs, fmt.Sprintf("replicas=%v", v))
	}

	if image, ok := node.Metadata["image"].(string); ok && image != "" {
		attrs = append(attrs, "image="+dotQuote(image))
	}

	if mode, ok := node.Metadata["mode"].(string); ok && mode != "" {
		attrs = append(attrs, "mode="+dotQuote(mode))
	}

	return fmt.Sprintf("%s [%s]", dotQuote(urn), strings.Join(attrs, " "))
}

// extractNetworkNames collects network names from edge metadata.
func extractNetworkNames(meta jgf.Metadata) string {
	names := make([]string, 0)

	switch v := meta["networks"].(type) {
	case []any:
		for _, entry := range v {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}

			name, _ := m["name"].(string)
			if name != "" {
				names = append(names, name)
			}
		}
	case []string:
		names = v
	}

	return strings.Join(names, ", ")
}

// dotQuote produces a DOT-safe quoted string. DOT strings are enclosed in
// double quotes with only `"` and `\` escaped (no \uXXXX sequences).
func dotQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')

	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}

	b.WriteByte('"')

	return b.String()
}
