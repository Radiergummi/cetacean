package jgf

import (
	"testing"

	json "github.com/goccy/go-json"
)

func TestURN(t *testing.T) {
	tests := []struct {
		typ  string
		id   string
		want string
	}{
		{"service", "abc123", "urn:cetacean:service:abc123"},
		{"node", "xyz789", "urn:cetacean:node:xyz789"},
		{"task", "t1", "urn:cetacean:task:t1"},
		{"network", "net0", "urn:cetacean:network:net0"},
	}

	for _, tc := range tests {
		got := URN(tc.typ, tc.id)
		if got != tc.want {
			t.Errorf("URN(%q, %q) = %q, want %q", tc.typ, tc.id, got, tc.want)
		}
	}
}

func TestDocument_MarshalJSON(t *testing.T) {
	doc := Document{
		Graphs: []Graph{
			{
				ID:       "g1",
				Type:     "network",
				Label:    "Test Graph",
				Directed: true,
				Metadata: Metadata{"@type": "NetworkTopology"},
				Nodes: map[string]Node{
					"n1": {Label: "node-one", Metadata: Metadata{"role": "manager"}},
					"n2": {Label: "node-two", Metadata: Metadata{"role": "worker"}},
				},
				Edges: []Edge{
					{Source: "n1", Target: "n2", Metadata: Metadata{"weight": 1}},
				},
			},
		},
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Document
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Graphs) != 1 {
		t.Fatalf("graphs len = %d, want 1", len(got.Graphs))
	}

	g := got.Graphs[0]
	if g.ID != "g1" {
		t.Errorf("id = %q, want g1", g.ID)
	}
	if g.Type != "network" {
		t.Errorf("type = %q, want network", g.Type)
	}
	if g.Label != "Test Graph" {
		t.Errorf("label = %q, want Test Graph", g.Label)
	}
	if !g.Directed {
		t.Errorf("directed = false, want true")
	}
	if len(g.Nodes) != 2 {
		t.Errorf("nodes len = %d, want 2", len(g.Nodes))
	}
	if g.Nodes["n1"].Label != "node-one" {
		t.Errorf("n1 label = %q, want node-one", g.Nodes["n1"].Label)
	}
	if len(g.Edges) != 1 {
		t.Errorf("edges len = %d, want 1", len(g.Edges))
	}
	if g.Edges[0].Source != "n1" || g.Edges[0].Target != "n2" {
		t.Errorf("edge source/target = %q/%q, want n1/n2", g.Edges[0].Source, g.Edges[0].Target)
	}
}

func TestGraph_WithEdgesAndHyperedges(t *testing.T) {
	ctx := Metadata{"@context": "/api/context.jsonld"}
	graph := Graph{
		ID:       "g2",
		Type:     "placement",
		Label:    "Placement Graph",
		Directed: false,
		Metadata: ctx,
		Nodes: map[string]Node{
			"a": {
				Label:    "alpha",
				Metadata: Metadata{"@context": "/api/context.jsonld", "kind": "node"},
			},
			"b": {
				Label:    "beta",
				Metadata: Metadata{"@context": "/api/context.jsonld", "kind": "node"},
			},
			"c": {
				Label:    "gamma",
				Metadata: Metadata{"@context": "/api/context.jsonld", "kind": "service"},
			},
		},
		Edges: []Edge{
			{
				Source:   "a",
				Target:   "b",
				Metadata: Metadata{"@context": "/api/context.jsonld", "type": "connects"},
			},
		},
		Hyperedges: []Hyperedge{
			{
				Nodes:    []string{"a", "b", "c"},
				Metadata: Metadata{"@context": "/api/context.jsonld", "group": "cluster-1"},
			},
		},
	}

	data, err := json.Marshal(graph)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Graph
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Edges) != 1 {
		t.Errorf("edges len = %d, want 1", len(got.Edges))
	}
	if len(got.Hyperedges) != 1 {
		t.Errorf("hyperedges len = %d, want 1", len(got.Hyperedges))
	}
	if len(got.Hyperedges[0].Nodes) != 3 {
		t.Errorf("hyperedge nodes len = %d, want 3", len(got.Hyperedges[0].Nodes))
	}
	if got.Hyperedges[0].Nodes[0] != "a" {
		t.Errorf("hyperedge node[0] = %q, want a", got.Hyperedges[0].Nodes[0])
	}
}
