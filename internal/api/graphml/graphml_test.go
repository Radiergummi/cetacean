package graphml_test

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/api/graphml"
	"github.com/radiergummi/cetacean/internal/api/jgf"
)

func TestRender_BasicGraph(t *testing.T) {
	g := jgf.Graph{
		ID:    "network",
		Label: "Network Topology",
		Nodes: map[string]jgf.Node{
			jgf.URN("service", "svc1"): {
				Label: "web",
				Metadata: jgf.Metadata{
					"kind":     "service",
					"replicas": 2,
					"image":    "nginx:latest",
					"mode":     "replicated",
				},
			},
			jgf.URN("service", "svc2"): {
				Label: "api",
				Metadata: jgf.Metadata{
					"kind":     "service",
					"replicas": 1,
					"image":    "api:latest",
					"mode":     "replicated",
				},
			},
		},
		Edges: []jgf.Edge{
			{
				Source: jgf.URN("service", "svc1"),
				Target: jgf.URN("service", "svc2"),
				Metadata: jgf.Metadata{
					"networks": []string{"myoverlay"},
				},
			},
		},
		Hyperedges: []jgf.Hyperedge{
			{
				Nodes: []string{jgf.URN("service", "svc1"), jgf.URN("service", "svc2")},
				Metadata: jgf.Metadata{
					"kind": "stack",
					"name": "mystack",
				},
			},
		},
	}

	out, err := graphml.Render(g)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	s := string(out)

	// Valid XML declaration
	if !strings.Contains(s, `<?xml`) {
		t.Error("missing XML declaration")
	}

	// Root element
	if !strings.Contains(s, `<graphml`) {
		t.Error("missing <graphml> root element")
	}

	// Key definitions
	if !strings.Contains(s, `id="label"`) {
		t.Error("missing key id=label")
	}
	if !strings.Contains(s, `id="kind"`) {
		t.Error("missing key id=kind")
	}

	// Root graph
	if !strings.Contains(s, `<graph id="network"`) {
		t.Error("missing root graph element")
	}

	// Stack subgraph
	if !strings.Contains(s, `<graph id="stack:mystack"`) {
		t.Error("missing stack subgraph")
	}
	if !strings.Contains(s, `<data key="label">mystack</data>`) {
		t.Error("missing stack label data element")
	}

	// Both service nodes present
	if !strings.Contains(s, `id="`+jgf.URN("service", "svc1")+`"`) {
		t.Error("missing svc1 node")
	}
	if !strings.Contains(s, `id="`+jgf.URN("service", "svc2")+`"`) {
		t.Error("missing svc2 node")
	}

	// Node data elements
	if !strings.Contains(s, `<data key="label">web</data>`) {
		t.Error("missing web node label data")
	}
	if !strings.Contains(s, `<data key="kind">service</data>`) {
		t.Error("missing kind data element")
	}

	// Edge
	if !strings.Contains(s, `<edge`) {
		t.Error("missing edge element")
	}
	if !strings.Contains(s, `<data key="networks">myoverlay</data>`) {
		t.Error("missing networks data on edge")
	}
}

func TestRender_EmptyGraph(t *testing.T) {
	g := jgf.Graph{
		ID:    "network",
		Label: "Empty",
		Nodes: map[string]jgf.Node{},
	}

	out, err := graphml.Render(g)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	s := string(out)

	if !strings.Contains(s, `<?xml`) {
		t.Error("missing XML declaration")
	}
	if !strings.Contains(s, `<graphml`) {
		t.Error("missing <graphml> root")
	}
	if !strings.Contains(s, `<graph id="network"`) {
		t.Error("missing root graph element")
	}
}

func TestRender_ServiceWithPorts(t *testing.T) {
	g := jgf.Graph{
		ID:    "network",
		Label: "Network Topology",
		Nodes: map[string]jgf.Node{
			jgf.URN("service", "svc1"): {
				Label: "web",
				Metadata: jgf.Metadata{
					"kind":  "service",
					"ports": []string{"80:80/tcp", "443:443/tcp"},
				},
			},
		},
	}

	out, err := graphml.Render(g)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	s := string(out)

	// Ports should be comma-separated in a single data element
	if !strings.Contains(s, `<data key="ports">80:80/tcp,443:443/tcp</data>`) {
		t.Errorf("expected comma-separated ports in data element, got:\n%s", s)
	}
}
