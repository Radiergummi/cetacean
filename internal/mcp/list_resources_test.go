package mcp

import (
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// seedListables puts one of each listable resource type in the cache.
func seedListables(t *testing.T) *cache.Cache {
	t.Helper()

	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1"})

	return c
}

// TestListResourcesReturnsTheWidgetEnvelope pins the shape the table widget
// renders. The widget is a separate artifact from the server and cannot be
// type-checked against it, so this envelope is the contract between them.
func TestListResourcesReturnsTheWidgetEnvelope(t *testing.T) {
	srv := newToolTestServer(t, seedListables(t), &fakeWriteClient{}, config.OpsReadOnly)
	handler := srv.Handler()

	_, env := mcpModern(t, handler, 1, "tools/call",
		`{"name":"list_resources","arguments":{"type":"services"}}`)
	if env.Error != nil {
		t.Fatalf("tools/call error: %+v", env.Error)
	}

	var result struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	if result.IsError {
		t.Fatalf("list_resources returned a tool error: %s", env.Result)
	}

	var listed listResourcesResult
	if err := json.Unmarshal(result.StructuredContent, &listed); err != nil {
		t.Fatalf("decode structured content: %v (raw %s)", err, result.StructuredContent)
	}

	if listed.Type != "services" {
		t.Errorf("type = %q, want %q", listed.Type, "services")
	}

	if listed.Total != 1 {
		t.Errorf("total = %d, want 1", listed.Total)
	}

	if len(listed.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(listed.Items))
	}
}

// TestListResourcesCoversEveryListableType — the widget offers a type picker,
// so a type the resource tree lists but the tool rejects is a dead entry in the
// UI. Iterating the whole set means adding a resource type without teaching the
// tool about it fails here rather than at a user.
func TestListResourcesCoversEveryListableType(t *testing.T) {
	srv := newToolTestServer(t, seedListables(t), &fakeWriteClient{}, config.OpsReadOnly)
	handler := srv.Handler()

	for _, resourceType := range listableResourceTypes {
		t.Run(resourceType, func(t *testing.T) {
			_, env := mcpModern(t, handler, 1, "tools/call",
				`{"name":"list_resources","arguments":{"type":"`+resourceType+`"}}`)
			if env.Error != nil {
				t.Fatalf("tools/call error: %+v", env.Error)
			}

			var result struct {
				IsError bool `json:"isError"`
			}
			if err := json.Unmarshal(env.Result, &result); err != nil {
				t.Fatalf("decode result: %v", err)
			}

			if result.IsError {
				t.Errorf("listing %q failed: %s", resourceType, env.Result)
			}
		})
	}
}

// TestListResourcesRejectsUnknownType keeps the tool from forwarding arbitrary
// strings into the resource URI dispatch.
func TestListResourcesRejectsUnknownType(t *testing.T) {
	srv := newToolTestServer(t, seedListables(t), &fakeWriteClient{}, config.OpsReadOnly)

	td, ok := srv.findTool("list_resources")
	if !ok {
		t.Fatal("list_resources not registered")
	}

	_, err := td.handler(t.Context(), newCallToolRequest("list_resources", map[string]any{
		"type": "../secrets",
	}))
	if err == nil {
		t.Fatal("expected an error for an unknown resource type")
	}
}

// TestListResourcesPages bounds what a widget pulls into a single frame.
func TestListResourcesPages(t *testing.T) {
	c := cache.New(nil)
	for _, id := range []string{"a", "b", "c"} {
		c.SetService(swarm.Service{
			ID:   id,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: id}},
		})
	}

	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsReadOnly)

	td, _ := srv.findTool("list_resources")

	out, err := td.handler(t.Context(), newCallToolRequest("list_resources", map[string]any{
		"type":   "services",
		"limit":  float64(2),
		"offset": float64(1),
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var listed listResourcesResult
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(listed.Items) != 2 {
		t.Errorf("got %d items, want 2 (limit)", len(listed.Items))
	}

	// Total reports the pre-paging count, so the widget can render "showing 2
	// of 3" without a second call.
	if listed.Total != 3 {
		t.Errorf("total = %d, want 3 (the count before paging)", listed.Total)
	}
}

// TestListResourcesToolPointsAtTheTableWidget is the half of the MCP Apps
// contract that lives on the tool: a host learns which view renders a result
// from the tool's _meta, so a widget nobody references is never shown.
func TestListResourcesToolPointsAtTheTableWidget(t *testing.T) {
	srv := newToolTestServer(t, seedListables(t), &fakeWriteClient{}, config.OpsReadOnly)

	td, ok := srv.findTool("list_resources")
	if !ok {
		t.Fatal("list_resources not registered")
	}

	if td.tool.Meta == nil {
		t.Fatal("list_resources carries no _meta; hosts cannot find its widget")
	}

	want := uiResourceURI("table")
	if got := td.tool.Meta.AdditionalFields[uiResourceURIMetaKey]; got != want {
		t.Errorf("_meta[%q] = %v, want %q", uiResourceURIMetaKey, got, want)
	}
}
