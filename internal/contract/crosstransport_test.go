package contract

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// addressable names the two ways a caller can address one seeded resource: by
// the ID Docker assigned and by the name a human uses. Both are addressing
// forms, not assertions about content — the invariant below is the equivalence,
// not the fixture.
type addressable struct {
	singular string // describe's spelling
	plural   string // find's spelling, and the REST path segment
	id       string
	name     string
}

func seededAddressables() []addressable {
	return []addressable{
		{"service", "services", SeededServiceID, SeededServiceName},
		{"node", "nodes", SeededNodeID, SeededNodeName},
		{"task", "tasks", SeededTaskID, ""},
		{"stack", "stacks", SeededStack, SeededStack},
		{"config", "configs", SeededConfigID, "shop_app-config"},
		{"secret", "secrets", SeededSecretID, "shop_app-secret"},
		{"network", "networks", SeededNetworkID, "shop_backend"},
		{"volume", "volumes", SeededVolumeName, SeededVolumeName},
	}
}

// TestEveryDescribableTypeIsAddressed fails when the product gains a
// describable type the sweep below does not address. Without it, I1 would
// quietly stop covering a new type.
func TestEveryDescribableTypeIsAddressed(t *testing.T) {
	w := NewWorld(t)
	catalog := MCPCatalog(t, w)

	addressed := make(map[string]bool)
	for _, a := range seededAddressables() {
		addressed[a.singular] = true
	}

	for _, describable := range catalog.DescribableTypes {
		if !addressed[describable] {
			t.Errorf(
				"describe accepts type %q but seededAddressables() has no entry "+
					"for it, so I1 does not cover it. Seed one and add it.",
				describable,
			)
		}
	}
}

// TestReadableOverRESTIsReadableOverMCP is I1. A resource either surface admits
// must be admitted by the other for the same identity: a divergence means one
// transport discloses something the other refuses, or refuses something the
// other serves, and a caller's answer then depends on which door they used.
func TestReadableOverRESTIsReadableOverMCP(t *testing.T) {
	w := NewWorld(t)

	for _, persona := range PersonaNames() {
		for _, resource := range seededAddressables() {
			for _, form := range []struct {
				label      string
				identifier string
			}{
				{"id", resource.id},
				{"name", resource.name},
			} {
				if form.identifier == "" {
					continue // this type has no name form; a task's is derived
				}

				name := fmt.Sprintf("%s/%s/by-%s", persona, resource.singular, form.label)

				t.Run(name, func(t *testing.T) {
					restOK := readableOverREST(t, w, persona, resource.plural, form.identifier)
					mcpOK := readableOverMCP(t, w, persona, resource.singular, form.identifier)

					if restOK == mcpOK {
						return
					}

					key := fmt.Sprintf("%s by %s", resource.singular, form.label)

					if reason, known := knownTransportDivergences[key]; known {
						t.Skipf("known divergence — %s", reason)
					}

					t.Errorf(
						"%s addressed by %s (%q): REST readable = %t, MCP readable "+
							"= %t. One transport admits what the other refuses, so "+
							"the answer a caller gets depends on which door they "+
							"used.",
						resource.singular, form.label, form.identifier, restOK, mcpOK,
					)
				})
			}
		}
	}
}

// readableOverREST reports whether the detail endpoint serves the resource.
//
// A name lookup answers 307 to the canonical ID URL (internal/api/canonical.go)
// rather than serving the resource directly, but w.Server.Client() is an
// ordinary *http.Client with no CheckRedirect override, so it follows the
// redirect itself — same-origin, so the persona headers ride along — and
// resp.StatusCode is already the final 200/403/404 by the time it gets here.
// The 307 branch stays as a defensive fallback in case that following behavior
// ever changes; it is not the path this test exercises today.
func readableOverREST(t *testing.T, w *World, persona, plural, identifier string) bool {
	t.Helper()

	resp := w.REST(t, persona, http.MethodGet, "/"+plural+"/"+identifier)
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusTemporaryRedirect:
		return true
	case http.StatusNotFound, http.StatusForbidden:
		return false
	default:
		t.Fatalf(
			"GET /%s/%s as %s = %d; readability is 200 (or a 307 to the "+
				"canonical URL) against 403/404, and any other status means "+
				"this invariant is measuring something else",
			plural, identifier, persona, resp.StatusCode,
		)

		return false
	}
}

// readableOverMCP reports whether describe returns the resource.
func readableOverMCP(t *testing.T, w *World, persona, singular, identifier string) bool {
	t.Helper()

	result, rpcErr := w.MCP(t, persona, "tools/call", map[string]any{
		"name": "describe",
		"arguments": map[string]any{
			"type": singular,
			"id":   identifier,
		},
	})
	if rpcErr != nil {
		return false
	}

	// mcp-go reports a refused call as a result with isError set, not as a
	// JSON-RPC error, so both have to be read.
	var call struct {
		IsError bool `json:"isError"`
	}

	if err := json.Unmarshal(result, &call); err != nil {
		t.Fatalf("decode tools/call describe: %v", err)
	}

	return !call.IsError
}
