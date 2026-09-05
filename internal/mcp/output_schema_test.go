package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/prom"
)

// TestCuratedToolsAdvertiseOutputSchema asserts that the tools whose result
// shape Cetacean owns advertise an outputSchema, so 2025-06-18+ clients know
// the structured shape to expect. Docker-passthrough mutations intentionally
// carry no schema (their result is a raw swarm.Service).
func TestCuratedToolsAdvertiseOutputSchema(t *testing.T) {
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

	schemas := map[string]json.RawMessage{}
	for _, tl := range result.Tools {
		schemas[tl.Name] = tl.OutputSchema
	}

	curated := map[string]bool{
		"find": true, "describe": true, "get_logs": true, "get_topology": true,
		"get_metrics": true, "get_recommendations": true,
		"remove_task": true, "remove_service": true, "remove_config": true,
		"remove_secret": true, "remove_network": true, "remove_volume": true,
		// The four lifecycle mutations return serviceMutationResult, a shape
		// Cetacean owns, rather than a raw swarm.Service.
		"scale_service": true, "update_service_image": true,
		"rollback_service": true, "restart_service": true,
	}

	hasSchema := func(s json.RawMessage) bool {
		return len(s) > 0 && string(s) != "null"
	}

	for name := range curated {
		schema, ok := schemas[name]
		if !ok {
			t.Errorf("%s not registered", name)
			continue
		}
		if !hasSchema(schema) {
			t.Errorf("%s: expected an advertised outputSchema, got %q", name, string(schema))
		}
	}

	// Enforce the whole contract, not a sample: every *other* registered tool is
	// a Docker-passthrough mutation and must NOT advertise a schema (validation
	// skips schemaless tools). Iterating the full registry means adding a tool
	// without deciding its schema status fails this test rather than silently
	// drifting.
	for name, schema := range schemas {
		if curated[name] {
			continue
		}
		if hasSchema(schema) {
			t.Errorf(
				"%s: non-curated tool must not advertise an outputSchema, got %q",
				name, string(schema),
			)
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
