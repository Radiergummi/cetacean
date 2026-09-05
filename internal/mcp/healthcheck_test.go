package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// healthcheckTestService is the shape these tests write against: a service
// with no healthcheck, which is the state get_recommendations reports for six
// of eight services on a real cluster and which nothing could previously fix.
func healthcheckTestService() swarm.Service {
	return swarm.Service{
		ID:   "svc1",
		Meta: swarm.Meta{Version: swarm.Version{Index: 41}},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:alpine"},
			},
		},
	}
}

// TestUpdateServiceSetsAHealthcheck closes the diagnose-without-treat gap:
// Cetacean reported which services had no health check and had no tool to add
// one.
func TestUpdateServiceSetsAHealthcheck(t *testing.T) {
	var got *container.HealthConfig

	svc := healthcheckTestService()
	c := cache.New(nil)
	c.SetService(svc)

	writeClient := &fakeWriteClient{
		updateServiceHealthFn: func(
			_ context.Context, _ string, hc *container.HealthConfig,
		) (swarm.Service, error) {
			got = hc

			updated := healthcheckTestService()
			updated.Spec.TaskTemplate.ContainerSpec.Healthcheck = hc

			return updated, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	result := callTool(t, handler, `{"name":"update_service","arguments":{"id":"web",`+
		`"section":"healthcheck","value":{"test":["CMD","curl","-f","http://localhost/"],`+
		`"interval":"10s","timeout":"3s","retries":3}}}`)

	if got == nil {
		t.Fatal("no healthcheck reached the Docker client")
	}
	if got.Interval != 10*time.Second {
		t.Errorf("interval = %v, want 10s", got.Interval)
	}
	if got.Timeout != 3*time.Second {
		t.Errorf("timeout = %v, want 3s", got.Timeout)
	}
	if got.Retries != 3 {
		t.Errorf("retries = %d, want 3", got.Retries)
	}
	if len(got.Test) != 4 || got.Test[0] != "CMD" {
		t.Errorf("test = %v, want the probe command", got.Test)
	}

	// The reply confirms the section it changed, and carries nothing else.
	var compact serviceUpdateResult
	if err := json.Unmarshal(result.StructuredContent, &compact); err != nil {
		t.Fatalf("decode: %v (%s)", err, result.StructuredContent)
	}

	if compact.Details["healthcheckInterval"] != "10s" {
		t.Errorf("details = %+v, want the healthcheck just set", compact.Details)
	}
	if _, leaked := compact.Details["envNames"]; leaked {
		t.Error("the reply carries fields outside the edited section")
	}
}

// A duration is the argument a model is most likely to get wrong, so the
// refusal has to name which of the three fields would not parse.
func TestUpdateServiceRejectsANonDurationInterval(t *testing.T) {
	c := cache.New(nil)
	c.SetService(healthcheckTestService())

	handler := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsConfiguration).Handler()

	// callTool fatals on an error result, which is what this test is here to
	// inspect, so it goes through the transport directly.
	_, envelope := mcpModern(t, handler, 1, "tools/call",
		`{"name":"update_service","arguments":{"id":"web",`+
			`"section":"healthcheck","value":{"test":["CMD","true"],"interval":"ten seconds"}}}`)

	var result toolCallResult
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode tool result: %v (raw %s)", err, envelope.Result)
	}

	if !result.IsError {
		t.Fatal("expected an error for an unparseable duration")
	}

	if !strings.Contains(string(envelope.Result), "interval") {
		t.Errorf("the error does not name the offending field: %s", envelope.Result)
	}

	// The write must not have been attempted: a refusal happens before Docker
	// is touched, which is why the fake stubs nothing.
	if strings.Contains(string(envelope.Result), "not stubbed") {
		t.Error("the tool reached the write client despite an invalid value")
	}
}

// An empty test removes the healthcheck rather than writing a probe that runs
// nothing — the only sensible reading, and one the description states.
func TestUpdateServiceRemovesAHealthcheckWithAnEmptyTest(t *testing.T) {
	var called bool
	var got *container.HealthConfig

	c := cache.New(nil)
	c.SetService(healthcheckTestService())

	writeClient := &fakeWriteClient{
		updateServiceHealthFn: func(
			_ context.Context, _ string, hc *container.HealthConfig,
		) (swarm.Service, error) {
			called = true
			got = hc

			return healthcheckTestService(), nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler, `{"name":"update_service","arguments":{"id":"web",`+
		`"section":"healthcheck","value":{"test":[]}}}`)

	if !called {
		t.Fatal("the write was skipped; an empty test must still clear the healthcheck")
	}
	if got != nil {
		t.Errorf("healthcheck = %+v, want nil to clear it", got)
	}
}

// TestUpdateServiceSetsTheCommand writes the entrypoint and its arguments as
// the separate things Docker treats them as.
func TestUpdateServiceSetsTheCommand(t *testing.T) {
	var applied swarm.ContainerSpec

	c := cache.New(nil)
	c.SetService(healthcheckTestService())

	writeClient := &fakeWriteClient{
		updateServiceContainerFn: func(
			_ context.Context, _ string, apply func(spec *swarm.ContainerSpec),
		) (swarm.Service, error) {
			spec := &swarm.ContainerSpec{Image: "nginx:alpine"}
			apply(spec)
			applied = *spec

			updated := healthcheckTestService()
			updated.Spec.TaskTemplate.ContainerSpec = spec

			return updated, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	result := callTool(t, handler, `{"name":"update_service","arguments":{"id":"web",`+
		`"section":"command","value":{"command":["sh","-c"],"args":["echo hi"]}}}`)

	if len(applied.Command) != 2 || applied.Command[0] != "sh" {
		t.Errorf("command = %v, want [sh -c]", applied.Command)
	}
	if len(applied.Args) != 1 || applied.Args[0] != "echo hi" {
		t.Errorf("args = %v, want [echo hi]", applied.Args)
	}

	// The image must survive a command edit: the mutator writes two fields and
	// must not reset the rest of the container spec.
	if applied.Image != "nginx:alpine" {
		t.Errorf("image = %q, want the untouched nginx:alpine", applied.Image)
	}

	var compact serviceUpdateResult
	if err := json.Unmarshal(result.StructuredContent, &compact); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := compact.Details["command"]; !ok {
		t.Errorf("details = %+v, want the command just set", compact.Details)
	}
}

// Both new sections must appear in the tool's advertised enum, or a caller is
// told the section does not exist.
func TestUpdateServiceAdvertisesTheNewSections(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	for _, def := range srv.toolCatalog() {
		if def.tool.Name != "update_service" {
			continue
		}

		schema, err := json.Marshal(def.tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal schema: %v", err)
		}

		for _, section := range []string{"healthcheck", "command"} {
			if !strings.Contains(string(schema), `"`+section+`"`) {
				t.Errorf("section %q missing from the advertised enum: %s", section, schema)
			}
		}

		return
	}

	t.Fatal("update_service is not registered")
}
