package contract

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cluster"
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

// readableOverREST reports whether the detail endpoint serves the resource. A
// name lookup answers 307 to the canonical ID URL, but the test client follows
// redirects itself — same-origin, so the persona headers ride along — and the
// status here is already the final one. The 307 branch is a defensive fallback.
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

// The dashboard confirms a drain from the REST assessment and an agent plans
// one from MCP's drain-impact view. Both read internal/cluster, so for one
// identity they must name the same services as movable and stranded.
func TestDrainImpactAgreesAcrossTransports(t *testing.T) {
	w := NewWorld(t)

	for _, persona := range PersonaNames() {
		t.Run(persona, func(t *testing.T) {
			resp := w.REST(t, persona, http.MethodGet, "/nodes/"+SeededNodeID+"/drain-impact")
			defer resp.Body.Close()

			result, rpcErr := w.MCP(t, persona, "tools/call", map[string]any{
				"name": "get_topology",
				"arguments": map[string]any{
					"view": cluster.TopologyViewDrainImpact,
					"node": SeededNodeID,
				},
			})

			var call struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if rpcErr == nil {
				if err := json.Unmarshal(result, &call); err != nil {
					t.Fatalf("decode tools/call get_topology: %v", err)
				}
			}

			mcpOK := rpcErr == nil && !call.IsError
			if restOK := resp.StatusCode == http.StatusOK; restOK != mcpOK {
				t.Fatalf("REST status %d, MCP admitted = %t", resp.StatusCode, mcpOK)
			}
			if !mcpOK {
				return
			}

			var overREST, overMCP cluster.TopologyGraph
			if err := json.NewDecoder(resp.Body).Decode(&overREST); err != nil {
				t.Fatalf("decode REST: %v", err)
			}
			if len(call.Content) == 0 {
				t.Fatal("get_topology returned no content")
			}
			if err := json.Unmarshal([]byte(call.Content[0].Text), &overMCP); err != nil {
				t.Fatalf("decode MCP: %v", err)
			}

			if !reflect.DeepEqual(overREST, overMCP) {
				t.Errorf("REST and MCP disagree:\nREST %+v\nMCP  %+v", overREST, overMCP)
			}
		})
	}
}

// A task whose service is gone has no service name for a grant to match, so
// both transports authorize its removal under the one key left, task:<id> —
// under the shared policy and under a grant on services alone.
func TestOrphanedTaskRemovalAgreesAcrossTransports(t *testing.T) {
	const orphanID = "task-orphaned"

	policies := map[string]string{
		"dev-auth": aclPolicy,
		"service-only": `
grants:
  - resources: ["service:*"]
    audience: ["group:oncall"]
    permissions: ["read", "write"]
`,
	}

	for label, text := range policies {
		t.Run(label, func(t *testing.T) {
			w := NewWorld(t)
			w.Cache.SetTask(swarm.Task{
				ID:        orphanID,
				ServiceID: "svc-removed",
				NodeID:    SeededNodeID,
				Status:    swarm.TaskStatus{State: swarm.TaskStateShutdown},
			})

			policy, err := acl.ParsePolicy([]byte(text))
			if err != nil {
				t.Fatalf("parse policy: %v", err)
			}
			w.ACL.SetPolicy(policy)

			for _, persona := range PersonaNames() {
				t.Run(persona, func(t *testing.T) {
					overREST := removableOverREST(t, w, persona, orphanID)
					overMCP := removableOverMCP(t, w, persona, orphanID)

					if overREST != overMCP {
						t.Fatalf("REST admitted = %t, MCP admitted = %t", overREST, overMCP)
					}
					if label == "service-only" && overREST {
						t.Fatal("a service grant admitted a write on a task with no service")
					}
				})
			}
		})
	}
}

// removableOverREST reports whether DELETE /tasks/{id} gets past authorization.
// A mismatched If-Match is answered 412 only once the ACL has admitted the
// caller, and stops the request before it reaches the absent Docker client.
func removableOverREST(t *testing.T, w *World, persona, id string) bool {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodDelete, w.Server.URL+"/tasks/"+id, nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("If-Match", `"mismatch"`)
	setPersonaHeaders(t, req, persona)

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE /tasks/%s: %v", id, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPreconditionFailed:
		return true
	case http.StatusForbidden, http.StatusNotFound:
		return false
	default:
		t.Fatalf("DELETE /tasks/%s as %s = %d; want 412, 403 or 404",
			id, persona, resp.StatusCode)

		return false
	}
}

// removableOverMCP reports whether remove_task gets past authorization. The
// world has no Docker client, so an admitted call fails on its absence. A tool
// tools/list hides from the caller is refused before it is called.
func removableOverMCP(t *testing.T, w *World, persona, id string) bool {
	t.Helper()

	listed, rpcErr := w.MCP(t, persona, "tools/list", nil)
	if rpcErr != nil {
		t.Fatalf("tools/list as %s: %s", persona, rpcErr.Message)
	}

	var catalog struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listed, &catalog); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	if !slices.ContainsFunc(catalog.Tools, func(tool struct {
		Name string `json:"name"`
	}) bool {
		return tool.Name == "remove_task"
	}) {
		return false
	}

	result, rpcErr := w.MCP(t, persona, "tools/call", map[string]any{
		"name":      "remove_task",
		"arguments": map[string]any{"id": id},
	})
	if rpcErr != nil {
		return false
	}

	var call struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &call); err != nil {
		t.Fatalf("decode tools/call remove_task: %v", err)
	}

	if !call.IsError || len(call.Content) == 0 {
		t.Fatalf("remove_task as %s succeeded without a Docker client", persona)
	}

	return strings.Contains(call.Content[0].Text, "no write client")
}
