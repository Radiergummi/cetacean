package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
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

// seedFilterFixture puts two of everything a post-filter test needs to tell
// apart into the cache: two services (different stacks, images and labels),
// two nodes (different states), and one task on each node against each
// service, so query/state/stack/node/image/label each have exactly one match
// to find and one to exclude.
func seedFilterFixture(t *testing.T) *cache.Cache {
	t.Helper()

	c := cache.New(nil)

	c.SetService(swarm.Service{
		ID: "svc-web",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name: "web",
				Labels: map[string]string{
					"com.docker.stack.namespace": "demo",
					"env":                        "prod",
					"team":                       "alpha",
				},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.21@sha256:aaa"},
			},
		},
	})
	c.SetService(swarm.Service{
		ID: "svc-worker",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name: "worker",
				Labels: map[string]string{
					"com.docker.stack.namespace": "other",
					"env":                        "dev",
				},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "redis:7@sha256:bbb"},
			},
		},
	})

	c.SetNode(swarm.Node{
		ID:          "node-a",
		Description: swarm.NodeDescription{Hostname: "node-a"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	c.SetNode(swarm.Node{
		ID:          "node-b",
		Description: swarm.NodeDescription{Hostname: "node-b"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateDown},
	})

	c.SetTask(swarm.Task{
		ID:        "task-web",
		ServiceID: "svc-web",
		NodeID:    "node-a",
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})
	c.SetTask(swarm.Task{
		ID:        "task-worker",
		ServiceID: "svc-worker",
		NodeID:    "node-b",
		Status:    swarm.TaskStatus{State: swarm.TaskStateFailed},
	})

	return c
}

// findRows drives find through the handler directly (post-filters are pure
// row-list logic that does not need the validating transport the schema
// tests use) and decodes the compact result.
func findRows(t *testing.T, srv *Server, args map[string]any) findResult {
	t.Helper()

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	out, err := td.handler(context.Background(), newCallToolRequest("find", args))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var got findResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}

	return got
}

// rowNames collects the Name of every row, for a compact assertion of which
// rows survived a filter.
func rowNames(rows []cluster.Row) []string {
	names := make([]string, len(rows))
	for i, row := range rows {
		names[i] = row.Name
	}
	return names
}

// TestFindQueryFilterNarrowsRows pins `query` as a post-filter over a typed
// listing (as opposed to findAcrossTypes, which uses query for the
// cross-type search covered separately by TestToolFindSearchesAcrossTypes).
func TestFindQueryFilterNarrowsRows(t *testing.T) {
	srv := newToolTestServer(t, seedFilterFixture(t), &fakeWriteClient{}, config.OpsReadOnly)

	got := findRows(t, srv, map[string]any{"type": "services", "query": "web"})

	if names := rowNames(got.Items); len(names) != 1 || names[0] != "web" {
		t.Errorf("query=web rows = %v, want [web]", names)
	}
}

// TestFindStateFilterNarrowsRows pins `state` against the node's raw
// swarm.NodeState — the field RowsForNodes copies into Row.State verbatim.
func TestFindStateFilterNarrowsRows(t *testing.T) {
	srv := newToolTestServer(t, seedFilterFixture(t), &fakeWriteClient{}, config.OpsReadOnly)

	got := findRows(t, srv, map[string]any{"type": "nodes", "state": "down"})

	if names := rowNames(got.Items); len(names) != 1 || names[0] != "node-b" {
		t.Errorf("state=down rows = %v, want [node-b]", names)
	}
}

// TestFindStackFilterNarrowsRows pins `stack` against the
// com.docker.stack.namespace label RowsForServices reads into Row.Stack.
func TestFindStackFilterNarrowsRows(t *testing.T) {
	srv := newToolTestServer(t, seedFilterFixture(t), &fakeWriteClient{}, config.OpsReadOnly)

	got := findRows(t, srv, map[string]any{"type": "services", "stack": "demo"})

	if names := rowNames(got.Items); len(names) != 1 || names[0] != "web" {
		t.Errorf("stack=demo rows = %v, want [web]", names)
	}
}

// TestFindNodeFilterNarrowsTaskRows pins `node`, which tests a task row's
// Detail — the node hostname RowsForTasks resolves the task onto.
func TestFindNodeFilterNarrowsTaskRows(t *testing.T) {
	srv := newToolTestServer(t, seedFilterFixture(t), &fakeWriteClient{}, config.OpsReadOnly)

	got := findRows(t, srv, map[string]any{"type": "tasks", "node": "node-b"})

	if len(got.Items) != 1 || got.Items[0].Detail != "node-b" {
		t.Errorf("node=node-b rows = %+v, want one row with Detail node-b", got.Items)
	}
}

// TestFindImageFilterNarrowsServiceRows pins `image`, which tests a service
// row's Detail — the digest-stripped image RowsForServices resolves.
func TestFindImageFilterNarrowsServiceRows(t *testing.T) {
	srv := newToolTestServer(t, seedFilterFixture(t), &fakeWriteClient{}, config.OpsReadOnly)

	got := findRows(t, srv, map[string]any{"type": "services", "image": "nginx"})

	if names := rowNames(got.Items); len(names) != 1 || names[0] != "web" {
		t.Errorf("image=nginx rows = %v, want [web]", names)
	}
}

// TestFindLabelFilterSupportsPresenceAndExactValue pins both forms Docker's
// own label-filter syntax supports: `key` alone tests presence, `key=value`
// tests an exact value — cluster.Row carries no label data at all, so this is
// the only filter that reads the pre-conversion record (labelsFor) rather
// than a Row field.
func TestFindLabelFilterSupportsPresenceAndExactValue(t *testing.T) {
	srv := newToolTestServer(t, seedFilterFixture(t), &fakeWriteClient{}, config.OpsReadOnly)

	// "team" only exists on svc-web.
	presence := findRows(t, srv, map[string]any{"type": "services", "label": "team"})
	if names := rowNames(presence.Items); len(names) != 1 || names[0] != "web" {
		t.Errorf("label=team rows = %v, want [web]", names)
	}

	// "env" exists on both, but only svc-web has env=prod.
	exact := findRows(t, srv, map[string]any{"type": "services", "label": "env=prod"})
	if names := rowNames(exact.Items); len(names) != 1 || names[0] != "web" {
		t.Errorf("label=env=prod rows = %v, want [web]", names)
	}

	// The exact-value form must reject a present key with the wrong value.
	mismatch := findRows(t, srv, map[string]any{"type": "services", "label": "env=staging"})
	if len(mismatch.Items) != 0 {
		t.Errorf("label=env=staging rows = %v, want none", rowNames(mismatch.Items))
	}
}

// TestFindRawModeHonoursFilters is the regression test for the bug where raw
// mode returned early, before rowFilters was built, and so ignored every
// post-filter — silently returning the whole type instead of just the
// caller's stack. raw only changes the *shape* of what comes back, never the
// scope, so the filtered set must be identical either way; this pins that the
// raw item surviving is genuinely the untouched record for the row that
// matched, not a coincidence of both being named "web".
func TestFindRawModeHonoursFilters(t *testing.T) {
	srv := newToolTestServer(t, seedFilterFixture(t), &fakeWriteClient{}, config.OpsReadOnly)

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	out, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"type":  "services",
		"stack": "demo",
		"raw":   true,
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var raw findRawResult
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}

	if raw.Total != 1 || len(raw.Items) != 1 {
		t.Fatalf(
			"stack=demo raw total/items = %d/%d, want 1/1 — the filter was not applied",
			raw.Total,
			len(raw.Items),
		)
	}

	// The surviving item must be the *untouched* svc-web record: its digest
	// is still on the image (RowsForServices strips it; raw must not), and
	// its worker sibling's fields must not leak in.
	item, err := json.Marshal(raw.Items[0])
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}

	var svc swarm.Service
	if err := json.Unmarshal(item, &svc); err != nil {
		t.Fatalf("unmarshal item as swarm.Service: %v", err)
	}

	if svc.ID != "svc-web" {
		t.Errorf("raw item ID = %q, want svc-web", svc.ID)
	}
	if svc.Spec.TaskTemplate.ContainerSpec.Image != "nginx:1.21@sha256:aaa" {
		t.Errorf(
			"raw item image = %q, want the untouched digest-bearing reference",
			svc.Spec.TaskTemplate.ContainerSpec.Image,
		)
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

// find and describe must disclose the same thing about the same task. A task
// row names its parent service and the node it runs on, and describe routes
// both through the caller's read grants (readableService/readableNode) so a
// digest cannot become a side channel for a resource the caller may not list.
// find passed the unfiltered cache listings into RowsForTasks, so it named a
// service describe withheld — one tool answering around the other's grants.
func TestFindNamesATasksParentsOnlyWhenDescribeWould(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc-secret",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "classified-payroll"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node-1",
		Description: swarm.NodeDescription{Hostname: "vault-host"},
	})
	c.SetTask(swarm.Task{ID: "task-1", ServiceID: "svc-secret", NodeID: "node-1", Slot: 1})

	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"task:*"},
		Audience:    []string{"user:agent@example.com"},
		Permissions: []string{"read"},
	}}})

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = evaluator })
	ctx := auth.ContextWithIdentity(
		context.Background(),
		&auth.Identity{Subject: "agent@example.com"},
	)

	found, err := srv.toolFind(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{Arguments: map[string]any{"type": "tasks"}},
	})
	if err != nil {
		t.Fatalf("find: %v", err)
	}

	for _, name := range []string{"classified-payroll", "vault-host"} {
		if strings.Contains(found, name) {
			t.Errorf("find disclosed %q, which the caller has no grant to read: %s", name, found)
		}
	}

	// The IDs stay: the task record the caller may read carries them itself,
	// so withholding them would cost the caller a reference without hiding
	// anything.
	if !strings.Contains(found, "node-1") {
		t.Errorf("find dropped the node ID the task record already carries: %s", found)
	}
}

// A cross-type search reports a total the caller cannot reach the tail of:
// `limit` caps the hits per type, there is no offset, and one figure over
// eight types says nothing about where the matches are. Counts — the per-type
// breakdown cluster.Search already computes, and the field the HTTP search
// response carries — is what makes "137 matches, showing 6" legible instead of
// looking like a listing that lost most of itself.
func TestFindAcrossTypesReportsPerTypeCounts(t *testing.T) {
	c := cache.New(nil)

	// Two services and two nodes match, but limit lets one of each through.
	for _, name := range []string{"web-frontend", "web-backend"} {
		c.SetService(swarm.Service{
			ID:   "svc-" + name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	for _, host := range []string{"web-node-a", "web-node-b"} {
		c.SetNode(swarm.Node{
			ID:          "node-" + host,
			Description: swarm.NodeDescription{Hostname: host},
		})
	}

	srv := newToolTestServer(t, c, nil, config.OpsReadOnly)
	td, _ := srv.findTool("find")

	out, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"query": "web",
		"limit": float64(1),
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var result findResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}

	if len(result.Items) != 2 {
		t.Fatalf("items = %d, want one per matching type (limit is per type)", len(result.Items))
	}

	if result.Total != 4 {
		t.Errorf("total = %d, want 4 — every match, not just the shown ones", result.Total)
	}

	if got := result.Counts["services"]; got != 2 {
		t.Errorf("counts[services] = %d, want 2", got)
	}

	if got := result.Counts["nodes"]; got != 2 {
		t.Errorf("counts[nodes] = %d, want 2", got)
	}
}

// Counts answers a question a typed listing does not have: there, total means
// one type and `offset` reaches the rest, so a breakdown would be a map with
// one key restating the number beside it.
func TestFindTypedListingCarriesNoCounts(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	srv := newToolTestServer(t, c, nil, config.OpsReadOnly)
	td, _ := srv.findTool("find")

	out, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"type": "services",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if strings.Contains(out, `"counts"`) {
		t.Errorf("typed listing carried counts: %s", out)
	}
}
