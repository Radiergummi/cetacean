package contract

import (
	"encoding/json"
	"slices"
	"testing"
)

// Tool is one entry from tools/list, with the schemas kept raw: an invariant
// asks structural questions of them that a decoded Go type would have to
// anticipate.
type Tool struct {
	Name         string          `json:"name"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

// Catalog is what the running MCP server advertises.
type Catalog struct {
	Tools             []Tool
	ResourceTemplates []string
	Prompts           []string

	// DescribableTypes is the enum on describe's `type` argument (singular);
	// FindableTypes is the enum on find's (plural). Both are read from the
	// live schema rather than declared here, so a type added to the product's
	// one map appears in both without this file changing.
	DescribableTypes []string
	FindableTypes    []string
}

// MCPCatalog enumerates the server over its own transport. Reading the catalog
// the way a client does is the point: a tool the server will not advertise is
// not part of the surface, however it is registered internally.
func MCPCatalog(t *testing.T, w *World) Catalog {
	t.Helper()

	var catalog Catalog

	toolsResult, rpcErr := w.MCP(t, "ops", "tools/list", nil)
	if rpcErr != nil {
		t.Fatalf("tools/list: %s", rpcErr.Message)
	}

	var tools struct {
		Tools []Tool `json:"tools"`
	}

	if err := json.Unmarshal(toolsResult, &tools); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	catalog.Tools = tools.Tools

	templatesResult, rpcErr := w.MCP(t, "ops", "resources/templates/list", nil)
	if rpcErr != nil {
		t.Fatalf("resources/templates/list: %s", rpcErr.Message)
	}

	var templates struct {
		ResourceTemplates []struct {
			URITemplate string `json:"uriTemplate"`
		} `json:"resourceTemplates"`
	}

	if err := json.Unmarshal(templatesResult, &templates); err != nil {
		t.Fatalf("decode resources/templates/list: %v", err)
	}

	for _, template := range templates.ResourceTemplates {
		catalog.ResourceTemplates = append(catalog.ResourceTemplates, template.URITemplate)
	}

	promptsResult, rpcErr := w.MCP(t, "ops", "prompts/list", nil)
	if rpcErr != nil {
		t.Fatalf("prompts/list: %s", rpcErr.Message)
	}

	var prompts struct {
		Prompts []struct {
			Name string `json:"name"`
		} `json:"prompts"`
	}

	if err := json.Unmarshal(promptsResult, &prompts); err != nil {
		t.Fatalf("decode prompts/list: %v", err)
	}

	for _, prompt := range prompts.Prompts {
		catalog.Prompts = append(catalog.Prompts, prompt.Name)
	}

	catalog.DescribableTypes = enumArgument(t, catalog, "describe", "type")
	catalog.FindableTypes = enumArgument(t, catalog, "find", "type")

	slices.Sort(catalog.ResourceTemplates)
	slices.Sort(catalog.Prompts)

	return catalog
}

// enumArgument reads the `enum` declared on one argument of one tool's input
// schema. A tool or argument that does not exist is fatal rather than empty: an
// invariant sweeping an empty set passes without asserting anything, which is
// the failure mode this whole package exists to avoid.
func enumArgument(t *testing.T, catalog Catalog, tool, argument string) []string {
	t.Helper()

	index := slices.IndexFunc(catalog.Tools, func(candidate Tool) bool {
		return candidate.Name == tool
	})
	if index < 0 {
		t.Fatalf("tools/list does not advertise %q", tool)
	}

	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}

	if err := json.Unmarshal(catalog.Tools[index].InputSchema, &schema); err != nil {
		t.Fatalf("decode %s input schema: %v", tool, err)
	}

	property, ok := schema.Properties[argument]
	if !ok {
		t.Fatalf("%s has no %q argument", tool, argument)
	}

	if len(property.Enum) == 0 {
		t.Fatalf(
			"%s's %q argument declares no enum. A sweep over it would assert "+
				"nothing at all.",
			tool, argument,
		)
	}

	values := slices.Clone(property.Enum)
	slices.Sort(values)

	return values
}
