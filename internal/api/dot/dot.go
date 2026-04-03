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

	if g.ID != "" {
		fmt.Fprintf(&b, "graph %s {\n", dotQuote(g.ID))
	} else {
		b.WriteString("graph {\n")
	}
	if g.Label != "" {
		fmt.Fprintf(&b, "\tlabel=%s;\n", dotQuote(g.Label))
	}

	// Extract stack membership from hyperedges.
	stackGroups, stackMembership := jgf.StackGroups(g.Hyperedges)

	// Emit stack subgraphs.
	for _, stackName := range jgf.SortedStackNames(stackGroups) {
		fmt.Fprintf(&b, "\tsubgraph %s {\n", dotQuote("cluster_"+stackName))
		fmt.Fprintf(&b, "\t\tlabel=%s;\n", dotQuote(stackName))

		for _, urn := range stackGroups[stackName] {
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
		if _, inStack := stackMembership[urn]; !inStack {
			topLevel = append(topLevel, urn)
		}
	}

	sort.Strings(topLevel)

	for _, urn := range topLevel {
		fmt.Fprintf(&b, "\t%s;\n", nodeStatement(urn, g.Nodes[urn]))
	}

	// Emit edges.
	for _, edge := range g.Edges {
		edgeLabel := strings.Join(jgf.NetworkNames(edge.Metadata["networks"]), ", ")
		if edgeLabel != "" {
			fmt.Fprintf(&b, "\t%s -- %s [label=%s];\n",
				dotQuote(edge.Source), dotQuote(edge.Target), dotQuote(edgeLabel))
		} else {
			fmt.Fprintf(&b, "\t%s -- %s;\n", dotQuote(edge.Source), dotQuote(edge.Target))
		}
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
		attrs = append(attrs, "replicas="+dotQuote(fmt.Sprintf("%v", v)))
	}

	for _, key := range []string{"kind", "image", "mode", "updateStatus"} {
		if v, ok := node.Metadata[key].(string); ok && v != "" {
			attrs = append(attrs, key+"="+dotQuote(v))
		}
	}

	if csv := jgf.JoinSlice(node.Metadata["ports"]); csv != "" {
		attrs = append(attrs, "ports="+dotQuote(csv))
	}

	return fmt.Sprintf("%s [%s]", dotQuote(urn), strings.Join(attrs, " "))
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
