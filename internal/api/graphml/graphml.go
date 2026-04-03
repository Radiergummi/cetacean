// Package graphml renders a [jgf.Graph] as GraphML XML.
package graphml

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/jgf"
)

// Key attribute-for values used in GraphML key definitions.
const (
	attrForNode  = "node"
	attrForEdge  = "edge"
	attrForGraph = "graph"
	attrTypeStr  = "string"
	attrTypeInt  = "int"
)

// graphmlDoc is the top-level GraphML document structure.
type graphmlDoc struct {
	XMLName xml.Name  `xml:"graphml"`
	XMLNS   string    `xml:"xmlns,attr"`
	Keys    []keyDef  `xml:"key"`
	Graph   graphElem `xml:"graph"`
}

type keyDef struct {
	ID       string `xml:"id,attr"`
	For      string `xml:"for,attr"`
	AttrName string `xml:"attr.name,attr"`
	AttrType string `xml:"attr.type,attr"`
}

// graphElem represents a <graph> element. It can contain group nodes
// (for stack hyperedges), regular nodes, and edges.
type graphElem struct {
	ID          string      `xml:"id,attr"`
	EdgeDefault string      `xml:"edgedefault,attr"`
	Data        []dataElem  `xml:"data,omitempty"`
	GroupNodes  []groupNode `xml:",omitempty"`
	Nodes       []nodeElem  `xml:",omitempty"`
	Edges       []edgeElem  `xml:",omitempty"`
}

// groupNode is a <node> containing a nested <graph> for stack grouping.
// Per the GraphML spec, nested graphs must be inside a <node> element.
type groupNode struct {
	XMLName xml.Name   `xml:"node"`
	ID      string     `xml:"id,attr"`
	Graph   groupGraph `xml:"graph"`
}

// groupGraph is the nested <graph> inside a groupNode.
type groupGraph struct {
	ID          string     `xml:"id,attr"`
	EdgeDefault string     `xml:"edgedefault,attr"`
	Data        []dataElem `xml:"data,omitempty"`
	Nodes       []nodeElem `xml:",omitempty"`
}

type nodeElem struct {
	XMLName xml.Name   `xml:"node"`
	ID      string     `xml:"id,attr"`
	Data    []dataElem `xml:"data,omitempty"`
}

type edgeElem struct {
	XMLName xml.Name   `xml:"edge"`
	Source  string     `xml:"source,attr"`
	Target  string     `xml:"target,attr"`
	Data    []dataElem `xml:"data,omitempty"`
}

type dataElem struct {
	XMLName xml.Name `xml:"data"`
	Key     string   `xml:"key,attr"`
	Value   string   `xml:",chardata"`
}

// Render converts a [jgf.Graph] to GraphML XML bytes.
//
// Stack hyperedges (kind=stack) become nested <graph> elements. Services that
// belong to a stack are declared inside their stack subgraph. Services without
// a stack are top-level <node> elements. Edges are always top-level.
// Output is deterministic: stacks sorted by name, nodes sorted by URN.
func Render(g jgf.Graph) ([]byte, error) {
	doc := graphmlDoc{
		XMLNS: "http://graphml.graphdrawing.org/graphml",
		Keys: []keyDef{
			{ID: "label", For: attrForNode, AttrName: "label", AttrType: attrTypeStr},
			{ID: "kind", For: attrForNode, AttrName: "kind", AttrType: attrTypeStr},
			{ID: "replicas", For: attrForNode, AttrName: "replicas", AttrType: attrTypeInt},
			{ID: "image", For: attrForNode, AttrName: "image", AttrType: attrTypeStr},
			{ID: "mode", For: attrForNode, AttrName: "mode", AttrType: attrTypeStr},
			{ID: "ports", For: attrForNode, AttrName: "ports", AttrType: attrTypeStr},
			{ID: "updateStatus", For: attrForNode, AttrName: "updateStatus", AttrType: attrTypeStr},
			{ID: "networks", For: attrForEdge, AttrName: "networks", AttrType: attrTypeStr},
			{ID: "graph-label", For: attrForGraph, AttrName: "label", AttrType: attrTypeStr},
		},
		Graph: graphElem{
			ID:          g.ID,
			EdgeDefault: "undirected",
		},
	}

	if g.Label != "" {
		doc.Graph.Data = append(doc.Graph.Data, dataElem{Key: "graph-label", Value: g.Label})
	}

	// Extract stack membership from hyperedges.
	stackNodes, nodeStack := jgf.StackGroups(g.Hyperedges)

	for _, stackName := range jgf.SortedStackNames(stackNodes) {
		members := stackNodes[stackName]
		stackID := "stack:" + stackName

		gg := groupGraph{
			ID:          stackID,
			EdgeDefault: "undirected",
			Data:        []dataElem{{Key: "graph-label", Value: stackName}},
		}

		for _, urn := range members {
			node, ok := g.Nodes[urn]
			if !ok {
				continue
			}
			gg.Nodes = append(gg.Nodes, buildNodeElem(urn, node))
		}

		doc.Graph.GroupNodes = append(doc.Graph.GroupNodes, groupNode{
			ID:    stackID,
			Graph: gg,
		})
	}

	// Collect top-level nodes (those not in any stack), sorted by URN.
	allURNs := make([]string, 0, len(g.Nodes))
	for urn := range g.Nodes {
		allURNs = append(allURNs, urn)
	}
	sort.Strings(allURNs)

	for _, urn := range allURNs {
		if _, inStack := nodeStack[urn]; inStack {
			continue
		}
		doc.Graph.Nodes = append(doc.Graph.Nodes, buildNodeElem(urn, g.Nodes[urn]))
	}

	// Build edge elements.
	for _, e := range g.Edges {
		elem := edgeElem{
			Source: e.Source,
			Target: e.Target,
		}

		if networks := extractNetworks(e.Metadata["networks"]); networks != "" {
			elem.Data = append(elem.Data, dataElem{Key: "networks", Value: networks})
		}

		doc.Graph.Edges = append(doc.Graph.Edges, elem)
	}

	// Marshal to indented XML.
	xmlBytes, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("graphml: marshal: %w", err)
	}

	return append([]byte(xml.Header), xmlBytes...), nil
}

// buildNodeElem constructs a <node> element from a JGF node.
func buildNodeElem(urn string, node jgf.Node) nodeElem {
	elem := nodeElem{ID: urn}

	if node.Label != "" {
		elem.Data = append(elem.Data, dataElem{Key: "label", Value: node.Label})
	}

	if kind, _ := node.Metadata["kind"].(string); kind != "" {
		elem.Data = append(elem.Data, dataElem{Key: "kind", Value: kind})
	}

	if replicas, ok := node.Metadata["replicas"]; ok {
		switch r := replicas.(type) {
		case int:
			elem.Data = append(elem.Data, dataElem{Key: "replicas", Value: strconv.Itoa(r)})
		case float64:
			elem.Data = append(elem.Data, dataElem{Key: "replicas", Value: strconv.Itoa(int(r))})
		default:
			elem.Data = append(elem.Data, dataElem{Key: "replicas", Value: fmt.Sprintf("%v", r)})
		}
	}

	if image, _ := node.Metadata["image"].(string); image != "" {
		elem.Data = append(elem.Data, dataElem{Key: "image", Value: image})
	}

	if mode, _ := node.Metadata["mode"].(string); mode != "" {
		elem.Data = append(elem.Data, dataElem{Key: "mode", Value: mode})
	}

	if ports := extractPorts(node.Metadata["ports"]); ports != "" {
		elem.Data = append(elem.Data, dataElem{Key: "ports", Value: ports})
	}

	if status, _ := node.Metadata["updateStatus"].(string); status != "" {
		elem.Data = append(elem.Data, dataElem{Key: "updateStatus", Value: status})
	}

	return elem
}

// extractPorts converts a ports metadata value to a comma-separated string.
// Accepts []string or []any (after JSON round-trip).
func extractPorts(v any) string {
	if v == nil {
		return ""
	}

	switch ports := v.(type) {
	case []string:
		return strings.Join(ports, ",")
	case []any:
		parts := make([]string, 0, len(ports))
		for _, p := range ports {
			if s, ok := p.(string); ok {
				parts = append(parts, s)
			}
		}

		return strings.Join(parts, ",")
	}

	return ""
}

// extractNetworks converts an edge networks metadata value to a comma-separated string.
// Accepts []string, []any, or more complex network objects (uses Name field if available).
func extractNetworks(v any) string {
	if v == nil {
		return ""
	}

	switch networks := v.(type) {
	case []string:
		return strings.Join(networks, ",")
	case []any:
		parts := make([]string, 0, len(networks))
		for _, n := range networks {
			switch entry := n.(type) {
			case string:
				parts = append(parts, entry)
			case map[string]any:
				if name, ok := entry["name"].(string); ok && name != "" {
					parts = append(parts, name)
				}
			}
		}

		return strings.Join(parts, ",")
	}

	return ""
}
