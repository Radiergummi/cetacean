package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/prom"
)

// TestEveryToolAdvertisesOutputSchema asserts that every registered tool
// declares an outputSchema, so a 2025-06-18+ client knows the structured shape
// to expect before it calls.
//
// There is no exempt set any more. The eleven spec-editing mutations used to
// be one, on the reasoning that their result was the raw Docker object and so
// not a shape Cetacean owned; they now answer with the same compact projection
// describe builds, scoped to the section they edited, and declare it. Driving
// the whole registry rather than a list means a new tool has to decide its
// result shape to pass, instead of quietly shipping without one.
func TestEveryToolAdvertisesOutputSchema(t *testing.T) {
	c := cache.New(nil)
	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsImpactful)
	handler := srv.Handler()

	_, env := mcpModern(t, handler, 2, "tools/list", `{}`)
	if env.Error != nil {
		t.Fatalf("tools/list error: %+v", env.Error)
	}

	var result struct {
		Tools []struct {
			Name         string          `json:"name"`
			OutputSchema json.RawMessage `json:"outputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	if len(result.Tools) == 0 {
		t.Fatal("no tools registered")
	}

	for _, tool := range result.Tools {
		if len(tool.OutputSchema) == 0 || string(tool.OutputSchema) == "null" {
			t.Errorf("%s: no advertised outputSchema", tool.Name)
		}
	}
}

// TestCuratedToolOutputsValidate drives the curated tools through the real
// validating dispatch path (WithOutputSchemaValidation is enabled in
// production) and asserts each returns structured content without a validation
// error — proving the advertised schema accepts the handler's real output.
func TestCuratedToolOutputsValidate(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1"})

	mutated := func(_ context.Context, id string) (swarm.Service, error) {
		return swarm.Service{ID: id}, nil
	}
	wc := &fakeWriteClient{
		removeTaskFn: func(_ context.Context, _ string) error { return nil },
		scaleServiceFn: func(_ context.Context, id string, _ uint64) (swarm.Service, error) {
			return swarm.Service{ID: id}, nil
		},
		updateServiceImageFn: func(_ context.Context, id, _ string) (swarm.Service, error) {
			return swarm.Service{ID: id}, nil
		},
		rollbackServiceFn: mutated,
		restartServiceFn:  mutated,
	}
	// get_metrics needs something to query; the numbers do not matter here,
	// only that the handler's real output satisfies its advertised schema.
	srv := newToolTestServer(t, c, wc, config.OpsImpactful, func(o *Options) {
		o.Prometheus = &fakeQuerier{
			series: []prom.Series{{Points: []prom.Point{{Timestamp: 1788254400, Value: 1}}}},
		}
		o.Recommendations = &fakeRecommendationEngine{results: recommendationFixtures()}
	})
	handler := srv.Handler()

	calls := []struct {
		name string
		args string
	}{
		{"find", `{"type":"services"}`},
		{"get_logs", `{"service":"svc1"}`},
		{"get_topology", `{"view":"placement"}`},
		{"get_metrics", `{"target":"service","id":"svc1"}`},
		{"get_recommendations", `{}`},
		{"remove_task", `{"id":"task1"}`},
		{"scale_service", `{"id":"svc1","replicas":2}`},
		{"update_service_image", `{"id":"svc1","image":"nginx:1"}`},
		{"rollback_service", `{"id":"svc1"}`},
		{"restart_service", `{"id":"svc1"}`},
	}

	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			_, env := mcpModern(t, handler, 3, "tools/call",
				`{"name":"`+call.name+`","arguments":`+call.args+`}`)
			if env.Error != nil {
				t.Fatalf("tools/call %s transport error: %+v", call.name, env.Error)
			}

			var result struct {
				IsError           bool            `json:"isError"`
				StructuredContent json.RawMessage `json:"structuredContent"`
				Content           []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(env.Result, &result); err != nil {
				t.Fatalf("decode result: %v: %s", err, string(env.Result))
			}
			if result.IsError {
				t.Fatalf("%s returned an error result (schema validation failed?): %s",
					call.name, string(env.Result))
			}
			if len(result.StructuredContent) == 0 || string(result.StructuredContent) == "null" {
				t.Errorf("%s: missing structuredContent: %s", call.name, string(env.Result))
			}
		})
	}
}

// TestEveryToolResultConformsToItsOutputSchema drives every tool through the
// real transport and holds the rule a strict client enforces: a tool that
// advertises an outputSchema MUST answer with structuredContent conforming to
// it — on every call, whatever the arguments, since one tool has one schema.
//
// Both halves need the transport. Conformance is checked by the server's own
// WithOutputSchemaValidation, which turns a breach into an isError result; the
// presence of structuredContent is decided in registerTools, where the
// CallToolResult is assembled. A test calling td.handler directly sees
// neither, which is how find's and describe's raw modes came to answer with
// text content and no structuredContent at all: the server's validator skips
// a result that has none, so nothing here failed, while the reference client
// rejects it outright ("has an output schema but did not return structured
// content").
func TestEveryToolResultConformsToItsOutputSchema(t *testing.T) {
	c := seededDescribeCache()

	svc := swarm.Service{
		ID:   "svc1",
		Meta: swarm.Meta{Version: swarm.Version{Index: 7}},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:alpine"},
			},
		},
	}
	node := swarm.Node{ID: "node1", Description: swarm.NodeDescription{Hostname: "manager-1"}}

	writeClient := &fakeWriteClient{
		simulatedEnv:         map[string]string{"A": "1"},
		simulatedLabels:      map[string]string{"a": "1"},
		simulatedNodeLabels:  map[string]string{"a": "1"},
		scaleServiceFn:       func(context.Context, string, uint64) (swarm.Service, error) { return svc, nil },
		updateServiceImageFn: func(context.Context, string, string) (swarm.Service, error) { return svc, nil },
		rollbackServiceFn:    func(context.Context, string) (swarm.Service, error) { return svc, nil },
		restartServiceFn:     func(context.Context, string) (swarm.Service, error) { return svc, nil },
		removeServiceFn:      func(context.Context, string) error { return nil },
		removeTaskFn:         func(context.Context, string) error { return nil },
		removeConfigFn:       func(context.Context, string) error { return nil },
		removeSecretFn:       func(context.Context, string) error { return nil },
		removeNetworkFn:      func(context.Context, string) error { return nil },
		removeVolumeFn:       func(context.Context, string, bool) error { return nil },
		updateServiceEnvFn: func(context.Context, string, map[string]string) (swarm.Service, error) {
			return svc, nil
		},
		updateServiceLabelsFn: func(context.Context, string, map[string]string) (swarm.Service, error) {
			return svc, nil
		},
		updateNodeLabelsFn: func(context.Context, string, map[string]string) (swarm.Node, error) {
			return node, nil
		},
		updateNodeAvailFn: func(context.Context, string, swarm.NodeAvailability) (swarm.Node, error) {
			return node, nil
		},
		updateNodeRoleFn: func(context.Context, string, swarm.NodeRole) (swarm.Node, error) {
			return node, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsImpactful).Handler()

	_, listed := mcpModern(t, handler, 1, "tools/list", `{}`)
	if listed.Error != nil {
		t.Fatalf("tools/list: %+v", listed.Error)
	}

	var catalog struct {
		Tools []struct {
			Name         string          `json:"name"`
			OutputSchema json.RawMessage `json:"outputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listed.Result, &catalog); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	declaresSchema := map[string]bool{}
	for _, tool := range catalog.Tools {
		declaresSchema[tool.Name] = len(tool.OutputSchema) > 0 &&
			string(tool.OutputSchema) != "null"
	}

	// One call per tool, plus the argument combinations that change the shape
	// of the answer: find's three modes and describe's two, which is where a
	// per-tool schema and a per-call result can part company.
	calls := []struct {
		label, tool, args string
	}{
		{"find/typed", "find", `{"type":"services"}`},
		{"find/cross-type", "find", `{"query":"web"}`},
		{"find/raw", "find", `{"type":"services","raw":true}`},
		{"describe", "describe", `{"type":"service","id":"web"}`},
		{"describe/raw", "describe", `{"type":"service","id":"web","raw":true}`},
		{"get_topology/network", "get_topology", `{"view":"network"}`},
		{"get_topology/placement", "get_topology", `{"view":"placement"}`},
		{"get_recommendations", "get_recommendations", `{}`},
		{"scale_service", "scale_service", `{"id":"web","replicas":2}`},
		{"update_service_image", "update_service_image", `{"id":"web","image":"nginx:1.27"}`},
		{"rollback_service", "rollback_service", `{"id":"web"}`},
		{"restart_service", "restart_service", `{"id":"web"}`},
		{"remove_task", "remove_task", `{"id":"task1"}`},
		{"remove_service", "remove_service", `{"id":"web"}`},
		{"remove_config", "remove_config", `{"id":"cfg1"}`},
		{"remove_secret", "remove_secret", `{"id":"sec1"}`},
		{"remove_network", "remove_network", `{"id":"net1"}`},
		{"remove_volume", "remove_volume", `{"name":"data"}`},
		{"update_service_env", "update_service_env", `{"id":"web","env":{"A":"2"}}`},
		{"update_service_labels", "update_service_labels", `{"id":"web","labels":{"a":"2"}}`},
		{"update_node_labels", "update_node_labels", `{"id":"node1","labels":{"a":"2"}}`},
		{
			"update_node_availability",
			"update_node_availability",
			`{"id":"node1","availability":"drain"}`,
		},
		{"update_node_role", "update_node_role", `{"id":"node1","role":"worker"}`},
	}

	for _, call := range calls {
		t.Run(call.label, func(t *testing.T) {
			params := fmt.Sprintf(`{"name":%q,"arguments":%s}`, call.tool, call.args)

			_, env := mcpModern(t, handler, 2, "tools/call", params)
			if env.Error != nil {
				t.Fatalf("transport error: %+v", env.Error)
			}

			var result struct {
				IsError           bool            `json:"isError"`
				StructuredContent json.RawMessage `json:"structuredContent"`
			}
			if err := json.Unmarshal(env.Result, &result); err != nil {
				t.Fatalf("decode result: %v (%s)", err, env.Result)
			}

			// A schema breach arrives here, since mcp-go answers one with an
			// error result rather than a protocol error.
			if result.IsError {
				t.Fatalf("tool call failed: %s", env.Result)
			}

			structured := len(result.StructuredContent) > 0 &&
				string(result.StructuredContent) != "null"

			if declaresSchema[call.tool] && !structured {
				t.Errorf(
					"%s declares an outputSchema but returned no structuredContent; "+
						"a strict client rejects that", call.tool,
				)
			}
		})
	}
}
