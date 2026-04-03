package dot_test

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/api/dot"
	"github.com/radiergummi/cetacean/internal/api/jgf"
)

func TestRender_BasicGraph(t *testing.T) {
	g := jgf.Graph{
		Label: "Network Topology",
		Nodes: map[string]jgf.Node{
			"urn:cetacean:service:aaa": {
				Label: "frontend",
				Metadata: jgf.Metadata{
					"kind":     "service",
					"replicas": 2,
					"image":    "nginx:latest",
					"mode":     "replicated",
				},
			},
			"urn:cetacean:service:bbb": {
				Label: "backend",
				Metadata: jgf.Metadata{
					"kind":     "service",
					"replicas": 1,
					"image":    "myapp:1.0",
					"mode":     "replicated",
				},
			},
		},
		Edges: []jgf.Edge{
			{
				Source: "urn:cetacean:service:aaa",
				Target: "urn:cetacean:service:bbb",
				Metadata: jgf.Metadata{
					"networks": []any{
						map[string]any{"name": "mynet"},
					},
				},
			},
		},
		Hyperedges: []jgf.Hyperedge{
			{
				Nodes: []string{"urn:cetacean:service:aaa", "urn:cetacean:service:bbb"},
				Metadata: jgf.Metadata{
					"kind": "stack",
					"name": "webapp",
				},
			},
		},
	}

	out, err := dot.Render(g)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	result := string(out)

	if !strings.HasPrefix(result, "graph") {
		t.Errorf("output should start with 'graph', got: %q", result[:min(len(result), 20)])
	}

	if !strings.Contains(result, `subgraph "cluster_webapp"`) {
		t.Errorf("output should contain subgraph cluster_webapp:\n%s", result)
	}

	if !strings.Contains(result, `label="frontend"`) {
		t.Errorf("output should contain label for frontend:\n%s", result)
	}

	if !strings.Contains(result, `label="backend"`) {
		t.Errorf("output should contain label for backend:\n%s", result)
	}

	if !strings.Contains(result, " -- ") {
		t.Errorf("output should contain undirected edge '--':\n%s", result)
	}

	if !strings.Contains(result, "mynet") {
		t.Errorf("output should contain edge label 'mynet':\n%s", result)
	}
}

func TestRender_EmptyGraph(t *testing.T) {
	g := jgf.Graph{
		Label: "Empty",
		Nodes: map[string]jgf.Node{},
	}

	out, err := dot.Render(g)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	result := string(out)

	if !strings.HasPrefix(result, "graph") {
		t.Errorf("output should start with 'graph', got: %q", result[:min(len(result), 20)])
	}

	if !strings.Contains(result, "{") || !strings.Contains(result, "}") {
		t.Errorf("output should be a valid DOT block:\n%s", result)
	}
}

func TestRender_ServiceOutsideStack(t *testing.T) {
	g := jgf.Graph{
		Label: "Network Topology",
		Nodes: map[string]jgf.Node{
			"urn:cetacean:service:zzz": {
				Label: "standalone",
				Metadata: jgf.Metadata{
					"kind":     "service",
					"replicas": 3,
					"image":    "redis:7",
					"mode":     "replicated",
				},
			},
		},
	}

	out, err := dot.Render(g)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	result := string(out)

	if strings.Contains(result, "subgraph") {
		t.Errorf("output should not contain subgraph when no stacks present:\n%s", result)
	}

	if !strings.Contains(result, `"urn:cetacean:service:zzz"`) {
		t.Errorf("output should contain top-level node URN:\n%s", result)
	}

	if !strings.Contains(result, `label="standalone"`) {
		t.Errorf("output should contain node label:\n%s", result)
	}
}
