package contract

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestWorldAnswersOverREST(t *testing.T) {
	w := NewWorld(t)

	resp := w.REST(t, "ops", http.MethodGet, "/services")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /services = %d, body: %s", resp.StatusCode, body)
	}

	var collection struct {
		Items []struct {
			ID string `json:"ID"`
		} `json:"items"`
		Total int `json:"total"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&collection); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if collection.Total == 0 {
		t.Error("GET /services returned an empty collection; the cache is not seeded")
	}
}

func TestWorldAnswersOverMCP(t *testing.T) {
	w := NewWorld(t)

	result, rpcErr := w.MCP(t, "ops", "tools/list", nil)
	if rpcErr != nil {
		t.Fatalf("tools/list: %s", rpcErr.Message)
	}

	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(result, &listed); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	if len(listed.Tools) == 0 {
		t.Fatal("tools/list returned no tools; the MCP handler is not mounted")
	}

	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}

	for _, want := range []string{"find", "describe"} {
		if !slices.Contains(names, want) {
			t.Errorf("tools/list is missing %q; have %s", want, strings.Join(names, ", "))
		}
	}
}

// TestWorldAnswersResourcesReadOverMCP drives resources/read, not tools/call:
// mcp-go keys the Mcp-Name header on params.uri for this method rather than
// params.name, and a header derivation that only ever looks at params.name
// rejects every resources/read at the protocol layer before any handler runs.
func TestWorldAnswersResourcesReadOverMCP(t *testing.T) {
	w := NewWorld(t)

	result, rpcErr := w.MCP(t, "ops", "resources/read", map[string]any{
		"uri": "cetacean://services/" + SeededServiceID,
	})
	if rpcErr != nil {
		t.Fatalf("resources/read: %s", rpcErr.Message)
	}

	var read struct {
		Contents []struct {
			URI string `json:"uri"`
		} `json:"contents"`
	}

	if err := json.Unmarshal(result, &read); err != nil {
		t.Fatalf("decode resources/read: %v", err)
	}

	if len(read.Contents) == 0 {
		t.Fatal("resources/read returned no contents")
	}
}

func TestWorldSeparatesPersonas(t *testing.T) {
	// viewers hold read on everything and write on nothing. The Allow header on
	// a detail endpoint is computed from the real evaluator, so it is the
	// cheapest proof that the persona actually reached the ACL layer rather
	// than every request arriving as the same identity.
	admin := w1Allow(t, "ops")
	viewer := w1Allow(t, "viewers")

	if admin == viewer {
		t.Errorf(
			"ops and viewers received the same Allow header (%q); personas are "+
				"not reaching the evaluator, so every ACL sweep built on this "+
				"world would be vacuous",
			admin,
		)
	}
}

// w1Allow reads the Allow header a persona gets on a service detail endpoint.
// Detail endpoints use setAllow/resourceWriteMethods; list endpoints use
// setAllowList, which is constant for most types and cannot distinguish
// personas at all.
func w1Allow(t *testing.T, persona string) string {
	t.Helper()

	w := NewWorld(t)

	resp := w.REST(t, persona, http.MethodGet, "/services/"+SeededServiceID)
	defer resp.Body.Close()

	return resp.Header.Get("Allow")
}

func TestWorldRecordsTheRouteItReached(t *testing.T) {
	w := NewWorld(t)

	resp := w.REST(t, "ops", http.MethodGet, "/services")
	resp.Body.Close()

	if !slices.Contains(exercisedRoutes(), "GET /services") {
		t.Errorf(
			"the recorder did not observe GET /services. Task 6's coverage "+
				"self-enforcement rests entirely on it, so a silent recorder "+
				"would make that whole gate vacuous.\nrecorded: %v",
			exercisedRoutes(),
		)
	}
}

func TestMCPCatalogEnumeratesTheServer(t *testing.T) {
	w := NewWorld(t)
	catalog := MCPCatalog(t, w)

	if len(catalog.Tools) < 20 {
		t.Errorf(
			"MCPCatalog found %d tools; the server registers 27 across four "+
				"tiers. The enumeration is incomplete.",
			len(catalog.Tools),
		)
	}

	if len(catalog.Prompts) == 0 {
		t.Error("MCPCatalog found no prompts; the server registers six")
	}

	if len(catalog.ResourceTemplates) == 0 {
		t.Error("MCPCatalog found no resource templates; the server registers nine")
	}
}

func TestMCPCatalogReadsTheTypeEnums(t *testing.T) {
	w := NewWorld(t)
	catalog := MCPCatalog(t, w)

	// describe takes singular type names, find takes plural. They are two
	// readings of one map in the product, so the counts must agree — if they
	// ever do not, a type is findable under one spelling and describable under
	// neither, which is the exact failure the product's own comment warns about.
	if len(catalog.DescribableTypes) != len(catalog.FindableTypes) {
		t.Errorf(
			"describe accepts %d types (%v) and find accepts %d (%v); they are "+
				"derived from one map and must agree",
			len(catalog.DescribableTypes), catalog.DescribableTypes,
			len(catalog.FindableTypes), catalog.FindableTypes,
		)
	}

	for _, want := range []string{"service", "node", "task", "stack", "config", "secret", "network", "volume"} {
		if !slices.Contains(catalog.DescribableTypes, want) {
			t.Errorf("describe does not accept type %q; accepts %v", want, catalog.DescribableTypes)
		}
	}
}
