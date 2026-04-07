# Topology Export Formats (GraphML + DOT) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GraphML and DOT as content-negotiated export formats on `GET /topology`, rendering the network topology graph only.

**Architecture:** Two new packages (`internal/api/graphml/`, `internal/api/dot/`) each expose a pure `Render(jgf.Graph) ([]byte, error)` function. Content negotiation gains two new types. The `/topology` route handler dispatches to the appropriate renderer.

**Tech Stack:** Go stdlib `encoding/xml` (GraphML), `fmt`/`strings` (DOT), `github.com/radiergummi/cetacean/internal/api/jgf`

---

## File Structure

- **Create:** `internal/api/graphml/graphml.go` — GraphML renderer
- **Create:** `internal/api/graphml/graphml_test.go` — GraphML tests
- **Create:** `internal/api/dot/dot.go` — DOT renderer
- **Create:** `internal/api/dot/dot_test.go` — DOT tests
- **Modify:** `internal/api/negotiate.go` — `ContentTypeGraphML`, `ContentTypeDOT`, extensions, supported types
- **Modify:** `internal/api/router.go` — two new cases in `/topology` switch
- **Modify:** `internal/api/topology.go` — extract `buildNetworkGraphForExport` helper to avoid duplicating ACL+build logic

---

### Task 1: GraphML Renderer

**Files:**
- Create: `internal/api/graphml/graphml.go`
- Create: `internal/api/graphml/graphml_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/api/graphml/graphml_test.go`:

```go
package graphml

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/api/jgf"
)

func TestRender_BasicGraph(t *testing.T) {
	g := jgf.Graph{
		ID:       "network",
		Type:     "network-topology",
		Label:    "Network Topology",
		Directed: false,
		Metadata: jgf.Metadata{"@context": "/api/context.jsonld"},
		Nodes: map[string]jgf.Node{
			"urn:cetacean:service:svc1": {
				Label: "webapp-api",
				Metadata: jgf.Metadata{
					"@context": "/api/context.jsonld",
					"kind":     "service",
					"replicas": 3,
					"image":    "api:latest",
					"mode":     "replicated",
				},
			},
			"urn:cetacean:service:svc2": {
				Label: "webapp-web",
				Metadata: jgf.Metadata{
					"@context": "/api/context.jsonld",
					"kind":     "service",
					"replicas": 2,
					"image":    "web:latest",
					"mode":     "replicated",
				},
			},
		},
		Edges: []jgf.Edge{
			{
				Source: "urn:cetacean:service:svc1",
				Target: "urn:cetacean:service:svc2",
				Metadata: jgf.Metadata{
					"@context": "/api/context.jsonld",
					"networks": []any{
						map[string]any{"id": "urn:cetacean:network:net1", "name": "frontend", "driver": "overlay", "scope": "swarm"},
					},
				},
			},
		},
		Hyperedges: []jgf.Hyperedge{
			{
				Nodes: []string{"urn:cetacean:service:svc1", "urn:cetacean:service:svc2"},
				Metadata: jgf.Metadata{
					"@context": "/api/context.jsonld",
					"kind":     "stack",
					"name":     "webapp",
				},
			},
		},
	}

	data, err := Render(g)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	xml := string(data)

	// Must be valid XML with graphml root
	if !strings.Contains(xml, "<?xml") {
		t.Error("missing XML declaration")
	}
	if !strings.Contains(xml, "<graphml") {
		t.Error("missing graphml root element")
	}

	// Must contain both nodes
	if !strings.Contains(xml, `id="urn:cetacean:service:svc1"`) {
		t.Error("missing svc1 node")
	}
	if !strings.Contains(xml, `id="urn:cetacean:service:svc2"`) {
		t.Error("missing svc2 node")
	}

	// Must contain edge
	if !strings.Contains(xml, `source="urn:cetacean:service:svc1"`) {
		t.Error("missing edge source")
	}
	if !strings.Contains(xml, `target="urn:cetacean:service:svc2"`) {
		t.Error("missing edge target")
	}

	// Must contain stack subgraph
	if !strings.Contains(xml, `id="stack:webapp"`) {
		t.Error("missing stack subgraph")
	}

	// Must contain data keys
	if !strings.Contains(xml, `<data key="label">webapp-api</data>`) {
		t.Error("missing label data for svc1")
	}
	if !strings.Contains(xml, `<data key="replicas">3</data>`) {
		t.Error("missing replicas data")
	}
}

func TestRender_EmptyGraph(t *testing.T) {
	g := jgf.Graph{
		ID:       "network",
		Metadata: jgf.Metadata{},
		Nodes:    map[string]jgf.Node{},
	}

	data, err := Render(g)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(data), "<graphml") {
		t.Error("empty graph should still produce valid graphml")
	}
}

func TestRender_ServiceWithPorts(t *testing.T) {
	g := jgf.Graph{
		ID:       "network",
		Metadata: jgf.Metadata{},
		Nodes: map[string]jgf.Node{
			"urn:cetacean:service:svc1": {
				Label: "web",
				Metadata: jgf.Metadata{
					"kind":  "service",
					"ports": []any{"80:8080/tcp", "443:8443/tcp"},
				},
			},
		},
	}

	data, err := Render(g)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(data), "80:8080/tcp, 443:8443/tcp") {
		t.Error("ports should be comma-separated in data element")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/graphml/ -v`
Expected: Compilation error — package doesn't exist.

- [ ] **Step 3: Implement the GraphML renderer**

Create `internal/api/graphml/graphml.go`:

```go
// Package graphml renders a JGF graph as GraphML XML.
package graphml

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/jgf"
)

// Render serializes a JGF graph as GraphML XML.
func Render(g jgf.Graph) ([]byte, error) {
	doc := graphmlDoc{
		XMLNS: "http://graphml.graphstudio.org",
	}

	// Define attribute keys.
	nodeKeys := []string{"label", "kind", "replicas", "image", "mode", "ports", "updateStatus"}
	for _, k := range nodeKeys {
		attrType := "string"
		if k == "replicas" {
			attrType = "int"
		}
		doc.Keys = append(doc.Keys, keyDef{
			ID:       k,
			For:      "node",
			AttrName: k,
			AttrType: attrType,
		})
	}
	doc.Keys = append(doc.Keys, keyDef{
		ID: "label", For: "graph", AttrName: "label", AttrType: "string",
	})
	doc.Keys = append(doc.Keys, keyDef{
		ID: "networks", For: "edge", AttrName: "networks", AttrType: "string",
	})

	root := graph{
		ID:          g.ID,
		EdgeDefault: "undirected",
	}

	// Build stack membership: URN → stack name.
	stackMembers := make(map[string]string)
	stackNodes := make(map[string][]string) // stackName → []URN
	for _, he := range g.Hyperedges {
		kind, _ := he.Metadata["kind"].(string)
		name, _ := he.Metadata["name"].(string)
		if kind != "stack" || name == "" {
			continue
		}
		for _, urn := range he.Nodes {
			stackMembers[urn] = name
			stackNodes[name] = append(stackNodes[name], urn)
		}
	}

	// Sort stack names for deterministic output.
	stackOrder := make([]string, 0, len(stackNodes))
	for name := range stackNodes {
		stackOrder = append(stackOrder, name)
	}
	sort.Strings(stackOrder)

	// Sort all node URNs.
	urns := make([]string, 0, len(g.Nodes))
	for urn := range g.Nodes {
		urns = append(urns, urn)
	}
	sort.Strings(urns)

	// Emit stack subgraphs with their member nodes.
	emitted := make(map[string]bool)
	for _, stackName := range stackOrder {
		members := stackNodes[stackName]
		sort.Strings(members)

		sg := graph{
			ID:          "stack:" + stackName,
			EdgeDefault: "undirected",
			Data:        []dataElem{{Key: "label", Value: stackName}},
		}
		for _, urn := range members {
			if n, ok := g.Nodes[urn]; ok {
				sg.Nodes = append(sg.Nodes, buildNode(urn, n))
				emitted[urn] = true
			}
		}
		root.SubGraphs = append(root.SubGraphs, sg)
	}

	// Emit top-level nodes (not in any stack).
	for _, urn := range urns {
		if emitted[urn] {
			continue
		}
		root.Nodes = append(root.Nodes, buildNode(urn, g.Nodes[urn]))
	}

	// Emit edges.
	for i, e := range g.Edges {
		var networkNames []string
		if nets, ok := e.Metadata["networks"].([]any); ok {
			for _, n := range nets {
				if m, ok := n.(map[string]any); ok {
					if name, ok := m["name"].(string); ok {
						networkNames = append(networkNames, name)
					}
				}
			}
		}

		root.Edges = append(root.Edges, edge{
			ID:     fmt.Sprintf("e%d", i),
			Source: e.Source,
			Target: e.Target,
			Data:   []dataElem{{Key: "networks", Value: strings.Join(networkNames, ", ")}},
		})
	}

	doc.Graph = root

	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}

	return append([]byte(xml.Header), out...), nil
}

func buildNode(id string, n jgf.Node) node {
	nd := node{ID: id}
	nd.Data = append(nd.Data, dataElem{Key: "label", Value: n.Label})

	for _, key := range []string{"kind", "image", "mode", "updateStatus"} {
		if v, ok := n.Metadata[key].(string); ok && v != "" {
			nd.Data = append(nd.Data, dataElem{Key: key, Value: v})
		}
	}
	if v, ok := n.Metadata["replicas"]; ok {
		nd.Data = append(nd.Data, dataElem{Key: "replicas", Value: fmt.Sprintf("%v", v)})
	}
	if ports, ok := n.Metadata["ports"].([]any); ok && len(ports) > 0 {
		strs := make([]string, len(ports))
		for i, p := range ports {
			strs[i] = fmt.Sprintf("%v", p)
		}
		nd.Data = append(nd.Data, dataElem{Key: "ports", Value: strings.Join(strs, ", ")})
	}

	return nd
}

// XML types for GraphML serialization.

type graphmlDoc struct {
	XMLName xml.Name `xml:"graphml"`
	XMLNS   string   `xml:"xmlns,attr"`
	Keys    []keyDef `xml:"key"`
	Graph   graph    `xml:"graph"`
}

type keyDef struct {
	ID       string `xml:"id,attr"`
	For      string `xml:"for,attr"`
	AttrName string `xml:"attr.name,attr"`
	AttrType string `xml:"attr.type,attr"`
}

type graph struct {
	ID          string     `xml:"id,attr"`
	EdgeDefault string     `xml:"edgedefault,attr"`
	Data        []dataElem `xml:"data,omitempty"`
	SubGraphs   []graph    `xml:"graph,omitempty"`
	Nodes       []node     `xml:"node,omitempty"`
	Edges       []edge     `xml:"edge,omitempty"`
}

type node struct {
	ID   string     `xml:"id,attr"`
	Data []dataElem `xml:"data,omitempty"`
}

type edge struct {
	ID     string     `xml:"id,attr"`
	Source string     `xml:"source,attr"`
	Target string     `xml:"target,attr"`
	Data   []dataElem `xml:"data,omitempty"`
}

type dataElem struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/graphml/ -v`
Expected: All 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/graphml/
git commit -m "feat(graphml): add GraphML renderer for topology export"
```

---

### Task 2: DOT Renderer

**Files:**
- Create: `internal/api/dot/dot.go`
- Create: `internal/api/dot/dot_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/api/dot/dot_test.go`:

```go
package dot

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/api/jgf"
)

func TestRender_BasicGraph(t *testing.T) {
	g := jgf.Graph{
		ID:       "network",
		Type:     "network-topology",
		Label:    "Network Topology",
		Directed: false,
		Metadata: jgf.Metadata{"@context": "/api/context.jsonld"},
		Nodes: map[string]jgf.Node{
			"urn:cetacean:service:svc1": {
				Label: "webapp-api",
				Metadata: jgf.Metadata{
					"kind":     "service",
					"replicas": 3,
					"image":    "api:latest",
					"mode":     "replicated",
				},
			},
			"urn:cetacean:service:svc2": {
				Label: "webapp-web",
				Metadata: jgf.Metadata{
					"kind":     "service",
					"replicas": 2,
					"image":    "web:latest",
					"mode":     "replicated",
				},
			},
		},
		Edges: []jgf.Edge{
			{
				Source: "urn:cetacean:service:svc1",
				Target: "urn:cetacean:service:svc2",
				Metadata: jgf.Metadata{
					"networks": []any{
						map[string]any{"name": "frontend"},
					},
				},
			},
		},
		Hyperedges: []jgf.Hyperedge{
			{
				Nodes: []string{"urn:cetacean:service:svc1", "urn:cetacean:service:svc2"},
				Metadata: jgf.Metadata{
					"kind": "stack",
					"name": "webapp",
				},
			},
		},
	}

	data, err := Render(g)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	dot := string(data)

	if !strings.HasPrefix(dot, "graph") {
		t.Error("should start with 'graph' (undirected)")
	}
	if !strings.Contains(dot, `subgraph "cluster_webapp"`) {
		t.Error("missing stack subgraph")
	}
	if !strings.Contains(dot, `label="webapp-api"`) {
		t.Error("missing svc1 label")
	}
	if !strings.Contains(dot, `replicas=3`) {
		t.Error("missing svc1 replicas")
	}
	// Undirected edge
	if !strings.Contains(dot, " -- ") {
		t.Error("should use -- for undirected edges")
	}
	if !strings.Contains(dot, `label="frontend"`) {
		t.Error("missing edge network label")
	}
}

func TestRender_EmptyGraph(t *testing.T) {
	g := jgf.Graph{
		ID:       "network",
		Label:    "Empty",
		Metadata: jgf.Metadata{},
		Nodes:    map[string]jgf.Node{},
	}

	data, err := Render(g)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	dot := string(data)
	if !strings.HasPrefix(dot, "graph") {
		t.Error("empty graph should still produce valid DOT")
	}
}

func TestRender_ServiceOutsideStack(t *testing.T) {
	g := jgf.Graph{
		ID:       "network",
		Label:    "Test",
		Metadata: jgf.Metadata{},
		Nodes: map[string]jgf.Node{
			"urn:cetacean:service:svc1": {
				Label:    "standalone",
				Metadata: jgf.Metadata{"kind": "service", "replicas": 1, "mode": "replicated"},
			},
		},
	}

	data, err := Render(g)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	dot := string(data)

	// Node should be at top level, not in a subgraph
	if strings.Contains(dot, "subgraph") {
		t.Error("standalone service should not be in a subgraph")
	}
	if !strings.Contains(dot, `"urn:cetacean:service:svc1"`) {
		t.Error("missing node declaration")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/dot/ -v`
Expected: Compilation error — package doesn't exist.

- [ ] **Step 3: Implement the DOT renderer**

Create `internal/api/dot/dot.go`:

```go
// Package dot renders a JGF graph as Graphviz DOT format.
package dot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/jgf"
)

// Render serializes a JGF graph as DOT (Graphviz) format.
func Render(g jgf.Graph) ([]byte, error) {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("graph %q {\n", g.Label))

	// Build stack membership.
	stackMembers := make(map[string]string)    // URN → stack name
	stackNodes := make(map[string][]string)     // stackName → []URN
	for _, he := range g.Hyperedges {
		kind, _ := he.Metadata["kind"].(string)
		name, _ := he.Metadata["name"].(string)
		if kind != "stack" || name == "" {
			continue
		}
		for _, urn := range he.Nodes {
			stackMembers[urn] = name
			stackNodes[name] = append(stackNodes[name], urn)
		}
	}

	// Sort stack names for deterministic output.
	stackOrder := make([]string, 0, len(stackNodes))
	for name := range stackNodes {
		stackOrder = append(stackOrder, name)
	}
	sort.Strings(stackOrder)

	// Sort all URNs.
	urns := make([]string, 0, len(g.Nodes))
	for urn := range g.Nodes {
		urns = append(urns, urn)
	}
	sort.Strings(urns)

	// Emit stack subgraphs.
	emitted := make(map[string]bool)
	for _, stackName := range stackOrder {
		members := stackNodes[stackName]
		sort.Strings(members)

		b.WriteString(fmt.Sprintf("  subgraph %q {\n", "cluster_"+stackName))
		b.WriteString(fmt.Sprintf("    label=%q;\n", stackName))

		for _, urn := range members {
			if n, ok := g.Nodes[urn]; ok {
				b.WriteString("    ")
				writeNode(&b, urn, n)
				emitted[urn] = true
			}
		}

		b.WriteString("  }\n")
	}

	// Emit top-level nodes.
	for _, urn := range urns {
		if emitted[urn] {
			continue
		}
		b.WriteString("  ")
		writeNode(&b, urn, g.Nodes[urn])
	}

	// Emit edges.
	for _, e := range g.Edges {
		var networkNames []string
		if nets, ok := e.Metadata["networks"].([]any); ok {
			for _, n := range nets {
				if m, ok := n.(map[string]any); ok {
					if name, ok := m["name"].(string); ok {
						networkNames = append(networkNames, name)
					}
				}
			}
		}

		label := strings.Join(networkNames, ", ")
		b.WriteString(fmt.Sprintf("  %q -- %q [label=%q];\n", e.Source, e.Target, label))
	}

	b.WriteString("}\n")

	return []byte(b.String()), nil
}

func writeNode(b *strings.Builder, id string, n jgf.Node) {
	attrs := []string{fmt.Sprintf("label=%q", n.Label)}

	if v, ok := n.Metadata["replicas"]; ok {
		attrs = append(attrs, fmt.Sprintf("replicas=%v", v))
	}
	for _, key := range []string{"image", "mode"} {
		if v, ok := n.Metadata[key].(string); ok && v != "" {
			attrs = append(attrs, fmt.Sprintf("%s=%q", key, v))
		}
	}

	b.WriteString(fmt.Sprintf("%q [%s];\n", id, strings.Join(attrs, " ")))
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/dot/ -v`
Expected: All 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/dot/
git commit -m "feat(dot): add DOT/Graphviz renderer for topology export"
```

---

### Task 3: Content Negotiation + Router Dispatch

**Files:**
- Modify: `internal/api/negotiate.go`
- Modify: `internal/api/negotiate_test.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/topology.go`

- [ ] **Step 1: Add ContentTypeGraphML and ContentTypeDOT to negotiate.go**

Add to the const block after `ContentTypeJGF`:

```go
ContentTypeGraphML
ContentTypeDOT
```

Add String cases:

```go
case ContentTypeGraphML:
    return "GraphML"
case ContentTypeDOT:
    return "DOT"
```

Add to `supportedTypes`:

```go
{"application", "graphml+xml", ContentTypeGraphML},
{"text", "vnd.graphviz", ContentTypeDOT},
```

Add extension suffixes in the `negotiate` function (after `.jgf`):

```go
} else if strings.HasSuffix(path, ".graphml") {
    ct = ContentTypeGraphML
    path = strings.TrimSuffix(path, ".graphml")
    r.URL.Path = path
} else if strings.HasSuffix(path, ".dot") {
    ct = ContentTypeDOT
    path = strings.TrimSuffix(path, ".dot")
    r.URL.Path = path
}
```

- [ ] **Step 2: Add negotiate tests**

Add to `internal/api/negotiate_test.go`:

```go
func TestParseAccept_GraphML(t *testing.T) {
    ct := parseAccept("application/graphml+xml")
    if ct != ContentTypeGraphML {
        t.Errorf("got %v, want ContentTypeGraphML", ct)
    }
}

func TestParseAccept_DOT(t *testing.T) {
    ct := parseAccept("text/vnd.graphviz")
    if ct != ContentTypeDOT {
        t.Errorf("got %v, want ContentTypeDOT", ct)
    }
}

func TestNegotiate_GraphMLSuffix(t *testing.T) {
    var captured ContentType
    inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        captured = ContentTypeFromContext(r.Context())
        if r.URL.Path != "/topology" {
            t.Errorf("expected path /topology, got %s", r.URL.Path)
        }
        w.WriteHeader(http.StatusOK)
    })

    handler := negotiate(inner)
    req := httptest.NewRequest("GET", "/topology.graphml", nil)
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)

    if captured != ContentTypeGraphML {
        t.Errorf("expected ContentTypeGraphML, got %v", captured)
    }
}

func TestNegotiate_DOTSuffix(t *testing.T) {
    var captured ContentType
    inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        captured = ContentTypeFromContext(r.Context())
        if r.URL.Path != "/topology" {
            t.Errorf("expected path /topology, got %s", r.URL.Path)
        }
        w.WriteHeader(http.StatusOK)
    })

    handler := negotiate(inner)
    req := httptest.NewRequest("GET", "/topology.dot", nil)
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)

    if captured != ContentTypeDOT {
        t.Errorf("expected ContentTypeDOT, got %v", captured)
    }
}
```

- [ ] **Step 3: Add a helper to topology.go for building the network graph**

Extract the ACL + buildNetworkJGF logic into a helper that both `HandleTopology` and the export handlers can use:

```go
// buildACLFilteredNetworkGraph builds the network JGF graph with ACL filtering
// applied. Used by HandleTopology and export format handlers.
func (h *Handlers) buildACLFilteredNetworkGraph(r *http.Request) jgf.Graph {
    identity := auth.IdentityFromContext(r.Context())
    services := acl.Filter(
        h.acl, identity, "read",
        h.cache.ListServices(),
        func(s swarm.Service) string { return "service:" + s.Spec.Name },
    )
    networks := h.cache.ListNetworks()
    contextURL := absPath(r.Context(), jsonLDContext)
    return buildNetworkJGF(services, networks, contextURL)
}
```

Refactor `HandleTopology` to use this helper for its network graph (keep the placement graph build inline since only `HandleTopology` needs it).

- [ ] **Step 4: Update the /topology route dispatch**

In `internal/api/router.go`, update the `/topology` switch:

```go
mux.HandleFunc("GET /topology", func(w http.ResponseWriter, r *http.Request) {
    switch ContentTypeFromContext(r.Context()) {
    case ContentTypeHTML:
        spa.ServeHTTP(w, r)
    case ContentTypeJGF, ContentTypeJSON:
        h.HandleTopology(w, r)
    case ContentTypeGraphML:
        h.HandleTopologyGraphML(w, r)
    case ContentTypeDOT:
        h.HandleTopologyDOT(w, r)
    default:
        writeErrorCode(w, r, "API003", "unsupported media type for this endpoint")
    }
})
```

- [ ] **Step 5: Add the export handlers to topology.go**

```go
// HandleTopologyGraphML serves the network topology as GraphML XML.
func (h *Handlers) HandleTopologyGraphML(w http.ResponseWriter, r *http.Request) {
    if !h.requireAnyGrant(w, r) {
        return
    }
    g := h.buildACLFilteredNetworkGraph(r)
    data, err := graphml.Render(g)
    if err != nil {
        writeErrorCode(w, r, "API009", "failed to render GraphML")
        return
    }
    w.Header().Set("Content-Type", "application/graphml+xml")
    writeRawWithETag(w, r, data)
}

// HandleTopologyDOT serves the network topology as DOT (Graphviz) format.
func (h *Handlers) HandleTopologyDOT(w http.ResponseWriter, r *http.Request) {
    if !h.requireAnyGrant(w, r) {
        return
    }
    g := h.buildACLFilteredNetworkGraph(r)
    data, err := dot.Render(g)
    if err != nil {
        writeErrorCode(w, r, "API009", "failed to render DOT")
        return
    }
    w.Header().Set("Content-Type", "text/vnd.graphviz")
    writeRawWithETag(w, r, data)
}
```

You'll need a `writeRawWithETag` helper (or use the existing ETag pattern). Check if one exists in `etag.go`. If not, add one:

```go
// writeRawWithETag writes pre-rendered bytes with ETag caching.
func writeRawWithETag(w http.ResponseWriter, r *http.Request, data []byte) {
    etag := computeETag(data)
    w.Header().Set("ETag", etag)
    w.Header().Set("Cache-Control", "no-cache")

    if etagMatch(r.Header.Get("If-None-Match"), etag) {
        w.WriteHeader(http.StatusNotModified)
        return
    }

    w.WriteHeader(http.StatusOK)
    w.Write(data) //nolint:errcheck
}
```

Add the imports for `graphml` and `dot` packages to `topology.go`.

- [ ] **Step 6: Run all tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/... -v`
Expected: All pass, including the new negotiate tests.

- [ ] **Step 7: Commit**

```bash
git add internal/api/negotiate.go internal/api/negotiate_test.go internal/api/router.go internal/api/topology.go internal/api/etag.go
git commit -m "feat(api): add GraphML and DOT export on /topology via content negotiation"
```

---

### Task 4: Integration Tests for Export Handlers

**Files:**
- Modify: `internal/api/topology_jgf_test.go`

- [ ] **Step 1: Write tests for the export handlers**

```go
func TestHandleTopologyGraphML(t *testing.T) {
    c := cache.New(nil)
    c.SetNetwork(network.Summary{ID: "net1", Name: "frontend", Driver: "overlay"})
    c.SetService(swarm.Service{
        ID:   "svc1",
        Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
        Endpoint: swarm.Endpoint{
            VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
        },
    })

    h := newTestHandlers(t, withCache(c))
    req := httptest.NewRequest("GET", "/topology", nil)
    w := httptest.NewRecorder()
    h.HandleTopologyGraphML(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("status=%d, want 200", w.Code)
    }
    if ct := w.Header().Get("Content-Type"); ct != "application/graphml+xml" {
        t.Errorf("Content-Type=%q, want application/graphml+xml", ct)
    }
    if !strings.Contains(w.Body.String(), "<graphml") {
        t.Error("response should contain graphml")
    }
    if w.Header().Get("ETag") == "" {
        t.Error("expected ETag header")
    }
}

func TestHandleTopologyDOT(t *testing.T) {
    c := cache.New(nil)
    c.SetNetwork(network.Summary{ID: "net1", Name: "frontend", Driver: "overlay"})
    c.SetService(swarm.Service{
        ID:   "svc1",
        Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
        Endpoint: swarm.Endpoint{
            VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
        },
    })

    h := newTestHandlers(t, withCache(c))
    req := httptest.NewRequest("GET", "/topology", nil)
    w := httptest.NewRecorder()
    h.HandleTopologyDOT(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("status=%d, want 200", w.Code)
    }
    if ct := w.Header().Get("Content-Type"); ct != "text/vnd.graphviz" {
        t.Errorf("Content-Type=%q, want text/vnd.graphviz", ct)
    }
    if !strings.HasPrefix(w.Body.String(), "graph") {
        t.Error("response should start with 'graph'")
    }
    if w.Header().Get("ETag") == "" {
        t.Error("expected ETag header")
    }
}
```

Add `"strings"` to imports if not already present.

- [ ] **Step 2: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandleTopologyGraphML|TestHandleTopologyDOT" -v`
Expected: Both pass.

- [ ] **Step 3: Commit**

```bash
git add internal/api/topology_jgf_test.go
git commit -m "test(api): add integration tests for GraphML and DOT export handlers"
```

---

### Task 5: Full Verification

**Files:** None new.

- [ ] **Step 1: Run all Go tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... -count=1`
Expected: All pass.

- [ ] **Step 2: Run lint**

Run: `cd /Users/moritz/GolandProjects/cetacean && make lint`
Expected: Clean.

- [ ] **Step 3: Run format check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make fmt-check`
Expected: Clean.
