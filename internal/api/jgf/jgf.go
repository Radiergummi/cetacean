// Package jgf defines types for the JSON Graph Format (https://jsongraphformat.info/).
package jgf

import "sort"

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

// StackGroups extracts stack membership from hyperedges with kind=stack.
// Returns a map of stack name → sorted member URNs, and a reverse map of
// member URN → stack name. Stack names are sorted for deterministic iteration.
func StackGroups(
	hyperedges []Hyperedge,
) (stacks map[string][]string, membership map[string]string) {
	membership = make(map[string]string)
	stackMap := make(map[string][]string)

	for _, he := range hyperedges {
		kind, _ := he.Metadata["kind"].(string)
		name, _ := he.Metadata["name"].(string)
		if kind != "stack" || name == "" {
			continue
		}
		for _, urn := range he.Nodes {
			membership[urn] = name
			stackMap[name] = append(stackMap[name], urn)
		}
	}

	// Sort members within each stack.
	for _, members := range stackMap {
		sort.Strings(members)
	}

	stacks = stackMap
	return
}

// SortedStackNames returns the stack names from a StackGroups result in sorted order.
func SortedStackNames(stacks map[string][]string) []string {
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NetworkNames extracts network name strings from an edge metadata "networks"
// value. Handles both []string (from JSON round-trip) and []any containing
// map[string]any with a "name" key (from direct construction).
func NetworkNames(v any) []string {
	switch networks := v.(type) {
	case []string:
		return networks
	case []any:
		names := make([]string, 0, len(networks))
		for _, entry := range networks {
			switch e := entry.(type) {
			case string:
				names = append(names, e)
			case map[string]any:
				if name, ok := e["name"].(string); ok && name != "" {
					names = append(names, name)
				}
			}
		}
		return names
	}
	return nil
}
