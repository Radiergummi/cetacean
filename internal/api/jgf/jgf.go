// Package jgf defines types for the JSON Graph Format (https://jsongraphformat.info/).
package jgf

// Document is a multi-graph JGF document.
type Document struct {
	Graphs []Graph `json:"graphs"`
}

// Graph is a single JGF graph or hypergraph.
type Graph struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Label      string          `json:"label"`
	Directed   bool            `json:"directed"`
	Metadata   Metadata        `json:"metadata"`
	Nodes      map[string]Node `json:"nodes"`
	Edges      []Edge          `json:"edges,omitempty"`
	Hyperedges []Hyperedge     `json:"hyperedges,omitempty"`
}

// Node is a JGF graph node.
type Node struct {
	Label    string   `json:"label"`
	Metadata Metadata `json:"metadata"`
}

// Edge is a pairwise relationship between two nodes.
type Edge struct {
	Source   string   `json:"source"`
	Target   string   `json:"target"`
	Metadata Metadata `json:"metadata"`
}

// Hyperedge is a group relationship connecting multiple nodes.
type Hyperedge struct {
	Nodes    []string `json:"nodes"`
	Metadata Metadata `json:"metadata"`
}

// Metadata is a JSON-LD annotated metadata object.
type Metadata map[string]any

// URN returns a cetacean URN for the given entity type and ID.
func URN(typ, id string) string {
	return "urn:cetacean:" + typ + ":" + id
}
