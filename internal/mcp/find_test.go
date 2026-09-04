package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// find returns rows, not Docker objects: the whole point is the size of the
// answer, and a raw swarm.Service costs roughly 1.7k tokens each.
func TestFindReturnsCompactRows(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "demo_web",
				Labels: map[string]string{"com.docker.stack.namespace": "demo"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:alpine@sha256:abc"},
			},
		},
	})

	srv := newLogTestServer(t, c, &fakeLogStreamer{})

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	out, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"type": "services",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var got findResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}

	if got.Total != 1 || len(got.Items) != 1 {
		t.Fatalf("total/items = %d/%d, want 1/1", got.Total, len(got.Items))
	}

	row := got.Items[0]

	if row.Name != "demo_web" || row.Stack != "demo" || row.Detail != "nginx:alpine" {
		t.Errorf("row = %+v, want the compact shape with a digest-free image", row)
	}

	// The raw Docker keys must not be in the payload at all.
	if bytes := []byte(out); json.Valid(bytes) {
		var probe map[string]any
		_ = json.Unmarshal(bytes, &probe)

		if _, leaked := probe["Spec"]; leaked {
			t.Error("payload carries raw Docker fields")
		}
	}
}

// A type nobody wrote a builder for must fail loudly rather than return an
// empty list that reads as "no such resources".
func TestFindRejectsUnknownType(t *testing.T) {
	srv := newLogTestServer(t, cache.New(nil), &fakeLogStreamer{})
	td, _ := srv.findTool("find")

	_, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"type": "widgets",
	}))
	if err == nil {
		t.Fatal("expected an error for an unknown type")
	}
}

// The search tool once advertised a schema requiring Hits while emitting
// results, and no test compared the two. Every tool with an output schema gets
// this check.
func TestFindResultMatchesItsOutputSchema(t *testing.T) {
	srv := newLogTestServer(t, cache.New(nil), &fakeLogStreamer{})

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	schema, err := json.Marshal(td.tool.OutputSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}

	var parsed struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	out, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"type": "services",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	for _, key := range parsed.Required {
		if _, present := payload[key]; !present {
			t.Errorf("schema requires %q, result does not carry it", key)
		}
	}
}

// findResultItemSchemaKeys reads find's outputSchema down to the element
// schema mcp-go generates for each entry in `items` — required keys, and the
// full set of keys the element declares (the schema also sets
// additionalProperties: false, so any key outside this set is a violation
// too). Reading the schema itself, rather than hardcoding cluster.Row's field
// names, means a caller of this helper tracks the type if Row ever changes.
func findResultItemSchemaKeys(
	t *testing.T,
	td toolDef,
) (required []string, allowed map[string]bool) {
	t.Helper()

	raw, err := json.Marshal(td.tool.OutputSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}

	var parsed struct {
		Properties struct {
			Items struct {
				Items struct {
					Properties map[string]any `json:"properties"`
					Required   []string       `json:"required"`
				} `json:"items"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	element := parsed.Properties.Items.Items

	allowed = make(map[string]bool, len(element.Properties))
	for key := range element.Properties {
		allowed[key] = true
	}

	return element.Required, allowed
}

// TestFindCompactItemsMatchTheirElementSchema is the check
// TestFindResultMatchesItsOutputSchema does not do: that check only compares
// the envelope's own top-level required keys (type/items/total) against the
// payload, which findRawResult — a completely different shape — also
// satisfies. This drives the real, output-schema-validating dispatch (the
// same one TestCuratedToolOutputsValidate uses) and checks each returned Row
// against the *element* schema advertised for `items`: every key the element
// schema requires must be present, and no key outside what the element schema
// declares may appear (mirroring its additionalProperties: false).
func TestFindCompactItemsMatchTheirElementSchema(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsReadOnly)

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	required, allowed := findResultItemSchemaKeys(t, td)

	_, env := mcpModern(t, srv.Handler(), 1, "tools/call",
		`{"name":"find","arguments":{"type":"services"}}`)
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
		t.Fatalf("find returned a tool error: %s", env.Result)
	}

	var envelope struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(result.StructuredContent, &envelope); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if len(envelope.Items) == 0 {
		t.Fatal("nothing to check — the seeded service did not come back")
	}

	for _, item := range envelope.Items {
		for _, key := range required {
			if _, present := item[key]; !present {
				t.Errorf("item %+v missing required key %q", item, key)
			}
		}

		for key := range item {
			if !allowed[key] {
				t.Errorf("item %+v carries key %q the element schema does not declare", item, key)
			}
		}
	}
}

// TestFindRawModeDoesNotClaimStructuredContent guards the bug this whole
// check exists for: raw hands back the untouched resource record, which is
// not the compact Row shape find's outputSchema describes, and the server
// validates structuredContent against that schema
// (mcpserver.WithOutputSchemaValidation, server.go). Before markTextOnlyResult
// was wired into the raw branch, every raw call failed here with "output
// schema validation failed" over the real transport — invisible to a test
// that calls td.handler directly, since that bypasses validation entirely.
func TestFindRawModeDoesNotClaimStructuredContent(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsReadOnly)

	_, env := mcpModern(t, srv.Handler(), 1, "tools/call",
		`{"name":"find","arguments":{"type":"services","raw":true}}`)
	if env.Error != nil {
		t.Fatalf("tools/call error: %+v", env.Error)
	}

	var result struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	if result.IsError {
		t.Fatalf("raw find must not fail output-schema validation: %s", env.Result)
	}

	if len(result.StructuredContent) != 0 && string(result.StructuredContent) != "null" {
		t.Errorf(
			"raw find must not present structuredContent — it does not conform to find's schema, got %s",
			result.StructuredContent,
		)
	}

	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "web") {
		t.Errorf("raw find's untouched record missing from its text content: %s", env.Result)
	}
}

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

// TestFindReturnsTheWidgetEnvelope pins the shape the table widget renders.
// The widget is a separate artifact from the server and cannot be
// type-checked against it, so this envelope is the contract between them.
func TestFindReturnsTheWidgetEnvelope(t *testing.T) {
	srv := newToolTestServer(t, seedListables(t), &fakeWriteClient{}, config.OpsReadOnly)
	handler := srv.Handler()

	_, env := mcpModern(t, handler, 1, "tools/call",
		`{"name":"find","arguments":{"type":"services"}}`)
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
		t.Fatalf("find returned a tool error: %s", env.Result)
	}

	var found findResult
	if err := json.Unmarshal(result.StructuredContent, &found); err != nil {
		t.Fatalf("decode structured content: %v (raw %s)", err, result.StructuredContent)
	}

	if found.Type != "services" {
		t.Errorf("type = %q, want %q", found.Type, "services")
	}

	if found.Total != 1 {
		t.Errorf("total = %d, want 1", found.Total)
	}

	if len(found.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(found.Items))
	}
}

// TestFindCoversEveryListableType — the widget offers a type picker, so a
// type the resource tree lists but the tool rejects is a dead entry in the
// UI. Iterating the whole set means adding a resource type without teaching
// the tool about it fails here rather than at a user.
func TestFindCoversEveryListableType(t *testing.T) {
	srv := newToolTestServer(t, seedListables(t), &fakeWriteClient{}, config.OpsReadOnly)
	handler := srv.Handler()

	for _, resourceType := range listableResourceTypes {
		t.Run(resourceType, func(t *testing.T) {
			_, env := mcpModern(t, handler, 1, "tools/call",
				`{"name":"find","arguments":{"type":"`+resourceType+`"}}`)
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
				t.Errorf("finding %q failed: %s", resourceType, env.Result)
			}
		})
	}
}

// TestFindRejectsPathTraversalType keeps the tool from forwarding arbitrary
// strings into the resource URI dispatch.
func TestFindRejectsPathTraversalType(t *testing.T) {
	srv := newToolTestServer(t, seedListables(t), &fakeWriteClient{}, config.OpsReadOnly)

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	_, err := td.handler(t.Context(), newCallToolRequest("find", map[string]any{
		"type": "../secrets",
	}))
	if err == nil {
		t.Fatal("expected an error for an unknown resource type")
	}
}

// TestFindPages bounds what a widget pulls into a single frame.
func TestFindPages(t *testing.T) {
	c := cache.New(nil)
	for _, id := range []string{"a", "b", "c"} {
		c.SetService(swarm.Service{
			ID:   id,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: id}},
		})
	}

	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsReadOnly)

	td, _ := srv.findTool("find")

	out, err := td.handler(t.Context(), newCallToolRequest("find", map[string]any{
		"type":   "services",
		"limit":  float64(2),
		"offset": float64(1),
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var found findResult
	if err := json.Unmarshal([]byte(out), &found); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(found.Items) != 2 {
		t.Errorf("got %d items, want 2 (limit)", len(found.Items))
	}

	// Total reports the pre-paging count, so the widget can render "showing 2
	// of 3" without a second call.
	if found.Total != 3 {
		t.Errorf("total = %d, want 3 (the count before paging)", found.Total)
	}
}

// TestFindToolPointsAtTheTableWidget is the half of the MCP Apps contract
// that lives on the tool: a host learns which view renders a result from the
// tool's _meta, so a widget nobody references is never shown.
func TestFindToolPointsAtTheTableWidget(t *testing.T) {
	srv := newToolTestServer(t, seedListables(t), &fakeWriteClient{}, config.OpsReadOnly)

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	if td.tool.Meta == nil {
		t.Fatal("find carries no _meta; hosts cannot find its widget")
	}

	want := uiResourceURI("table")
	if got := td.tool.Meta.AdditionalFields[uiResourceURIMetaKey]; got != want {
		t.Errorf("_meta[%q] = %v, want %q", uiResourceURIMetaKey, got, want)
	}
}

// TestToolFindSearchesAcrossTypes is find's replacement for the deleted
// search tool: omitting `type` searches every resource type by name at once.
func TestToolFindSearchesAcrossTypes(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web-frontend"}},
	})

	srv := newToolTestServer(t, c, nil, config.OpsReadOnly)
	td, _ := srv.findTool("find")

	out, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"query": "web",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.Contains(out, "web-frontend") {
		t.Errorf("cross-type search output missing match: %s", out)
	}
}

// TestToolFindRejectsEmptyQueryWithoutType — a cross-type search with no
// query would otherwise return every resource in the cluster with no way to
// narrow it, indistinguishable from a caller who forgot the query entirely.
func TestToolFindRejectsEmptyQueryWithoutType(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), nil, config.OpsReadOnly)
	td, _ := srv.findTool("find")

	for _, q := range []string{"", "   ", "\t\n"} {
		_, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
			"query": q,
		}))
		if err == nil {
			t.Errorf("query %q: expected error, got nil", q)
		}
	}
}
