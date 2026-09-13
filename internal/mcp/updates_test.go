package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// serviceWithSecrets is a service holding the two things these tools must
// never hand back: environment variable values, and a log driver option that
// is a credential.
func serviceWithSecrets() swarm.Service {
	return swarm.Service{
		ID:   "svc1",
		Meta: swarm.Meta{Version: swarm.Version{Index: 41}},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: "nginx:alpine",
					Env: []string{
						"DATABASE_PASSWORD=hunter2",
						"API_TOKEN=sk-live-abc",
					},
				},
				Resources: &swarm.ResourceRequirements{
					Limits: &swarm.Limit{NanoCPUs: 2e9, MemoryBytes: 1 << 30},
				},
				LogDriver: &swarm.Driver{
					Name:    "splunk",
					Options: map[string]string{"splunk-token": "tok-secret"},
				},
			},
		},
	}
}

// dispatchedServiceSections is one valid argument per section update_service
// dispatches, package-level so the advertised list can be held against it both
// ways. What it guards is coverage: a section nobody calls is one whose result
// shape, and so whose disclosure, nothing checks.
var dispatchedServiceSections = map[string]string{
	sectionResources:      `{"Limits":{"NanoCPUs":1000000000}}`,
	sectionPlacement:      `{"Constraints":["node.role==worker"]}`,
	sectionPorts:          `[{"TargetPort":80}]`,
	sectionLabels:         `{"tier":"edge"}`,
	sectionEnv:            `{"DATABASE_PASSWORD":"hunter2"}`,
	sectionLogDriver:      `{"Name":"splunk","Options":{"splunk-token":"tok-secret"}}`,
	sectionUpdatePolicy:   `{"Parallelism":2}`,
	sectionRollbackPolicy: `{"Parallelism":1}`,
	sectionHealthcheck:    `{"test":["CMD-SHELL","curl -f localhost || exit 1"],"interval":"10s"}`,
	sectionCommand:        `{"command":["nginx"],"args":["-g","daemon off;"]}`,
}

// The bug this whole change exists for: a tool that only raises a CPU limit
// used to answer with the entire swarm.Service, so an agent asking to resize a
// service was handed its database password. Every one of these tools returned
// the raw object, and only one of them is about environment variables at all.
func TestSpecEditingToolsNeverReturnSecrets(t *testing.T) {
	svc := serviceWithSecrets()

	c := cache.New(nil)
	c.SetService(svc)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "manager-1"},
	})

	writeClient := &fakeWriteClient{
		simulatedEnv:    map[string]string{"DATABASE_PASSWORD": "hunter2"},
		simulatedLabels: map[string]string{"tier": "edge"},
		updateServiceResFn: func(
			context.Context, string, *swarm.ResourceRequirements,
		) (swarm.Service, error) {
			return svc, nil
		},
		updateServicePlaceFn: func(context.Context, string, *swarm.Placement) (swarm.Service, error) {
			return svc, nil
		},
		updateServicePortsFn: func(context.Context, string, []swarm.PortConfig) (swarm.Service, error) {
			return svc, nil
		},
		updateServiceEnvFn: func(context.Context, string, map[string]string) (swarm.Service, error) {
			return svc, nil
		},
		updateServiceLabelsFn: func(context.Context, string, map[string]string) (swarm.Service, error) {
			return svc, nil
		},
		updateServiceLogDrvFn: func(context.Context, string, *swarm.Driver) (swarm.Service, error) {
			return svc, nil
		},
		updateServiceUpdateFn: func(context.Context, string, *swarm.UpdateConfig) (swarm.Service, error) {
			return svc, nil
		},
		updateServiceRbackFn: func(context.Context, string, *swarm.UpdateConfig) (swarm.Service, error) {
			return svc, nil
		},
		updateServiceHealthFn: func(
			context.Context, string, *container.HealthConfig,
		) (swarm.Service, error) {
			return svc, nil
		},
		updateServiceContainerFn: func(
			_ context.Context, _ string, apply func(spec *swarm.ContainerSpec),
		) (swarm.Service, error) {
			apply(svc.Spec.TaskTemplate.ContainerSpec)

			return svc, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsImpactful).Handler()

	for section, value := range dispatchedServiceSections {
		t.Run(section, func(t *testing.T) {
			result := callTool(t, handler,
				`{"name":"update_service","arguments":{"id":"web","section":"`+
					section+`","value":`+value+`}}`)

			body, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}

			for _, secret := range []string{"hunter2", "sk-live-abc", "tok-secret"} {
				if strings.Contains(string(body), secret) {
					t.Errorf("result leaked %q: %s", secret, body)
				}
			}

			// The names must survive — an agent confirming an edit needs to
			// see which variables are set, only never what they are set to.
			var compact serviceUpdateResult
			if err := json.Unmarshal(result.StructuredContent, &compact); err != nil {
				t.Fatalf("decode: %v (%s)", err, result.StructuredContent)
			}

			if compact.ID != "svc1" || compact.Name != "web" {
				t.Errorf("identity missing: %+v", compact)
			}

			// A follow-up write needs the version Docker returned; reading it
			// back off the asynchronously-filled cache is the race this
			// avoids.
			if compact.Version != 41 {
				t.Errorf("version = %d, want 41", compact.Version)
			}
		})
	}
}

// Each tool answers about the section it edited, using the same projection
// describe builds — so confirming an edit and describing the resource
// afterwards cannot disagree.
func TestSpecEditingToolsReportTheEditedSection(t *testing.T) {
	svc := serviceWithSecrets()

	c := cache.New(nil)
	c.SetService(svc)

	writeClient := &fakeWriteClient{
		updateServiceResFn: func(
			context.Context, string, *swarm.ResourceRequirements,
		) (swarm.Service, error) {
			return svc, nil
		},
		updateServiceEnvFn: func(context.Context, string, map[string]string) (swarm.Service, error) {
			return svc, nil
		},
		simulatedEnv: map[string]string{},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsImpactful).Handler()

	resources := callTool(t, handler,
		`{"name":"update_service","arguments":{"id":"web","section":"resources",`+
			`"value":{"Limits":{"NanoCPUs":2000000000}}}}`)

	var got serviceUpdateResult
	if err := json.Unmarshal(resources.StructuredContent, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Section != sectionResources {
		t.Errorf("section = %q, want %q", got.Section, sectionResources)
	}

	if got.Details["cpuLimitCores"] != float64(2) {
		t.Errorf("cpuLimitCores = %v, want 2: %+v", got.Details["cpuLimitCores"], got.Details)
	}

	// The section is the *answer*, so a resources edit must not also report
	// the ports or the env — that is what made these results expensive.
	if _, ok := got.Details["envNames"]; ok {
		t.Errorf("a resources edit reported the env: %+v", got.Details)
	}

	env := callTool(t, handler,
		`{"name":"update_service","arguments":{"id":"web","section":"env","value":{"A":"1"}}}`)

	var envResult serviceUpdateResult
	if err := json.Unmarshal(env.StructuredContent, &envResult); err != nil {
		t.Fatalf("decode: %v", err)
	}

	names, _ := envResult.Details["envNames"].([]any)
	if len(names) != 2 {
		t.Fatalf("envNames = %v, want the two variable names", envResult.Details["envNames"])
	}
}

// A node edit reports the role and availability whichever section it touched:
// draining a manager is a different act from draining a worker, and "did the
// drain take" is not answerable without the role it took effect on.
func TestNodeUpdateReportsRoleAndAvailability(t *testing.T) {
	node := swarm.Node{
		ID:   "node1",
		Meta: swarm.Meta{Version: swarm.Version{Index: 9}},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityDrain,
		},
		Description: swarm.NodeDescription{Hostname: "manager-1"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	}

	c := cache.New(nil)
	c.SetNode(node)

	writeClient := &fakeWriteClient{
		updateNodeAvailFn: func(
			context.Context, string, swarm.NodeAvailability,
		) (swarm.Node, error) {
			return node, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsImpactful).Handler()

	result := callTool(
		t,
		handler,
		`{"name":"update_node","arguments":{"id":"node1","section":"availability","value":"drain"}}`,
	)

	var got nodeUpdateResult
	if err := json.Unmarshal(result.StructuredContent, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, result.StructuredContent)
	}

	if got.Hostname != "manager-1" || got.Version != 9 {
		t.Errorf("identity/version wrong: %+v", got)
	}

	if got.Role != "manager" {
		t.Errorf("role = %q, want manager", got.Role)
	}

	if got.Availability != "drain" {
		t.Errorf("availability = %q, want drain", got.Availability)
	}
}

// The attachment editors answer through serviceUpdate, so they need an entry in
// the projection table — but they are tools of their own and update_service
// cannot dispatch them. Validating against that table conflates the two: the
// call matches no case and returns a zero result that reads as a write.
func TestUpdateServiceRefusesTheAttachmentSections(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithSecrets())

	handler := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsImpactful).Handler()

	for _, section := range []string{sectionSecrets, sectionConfigs, sectionMounts} {
		t.Run(section, func(t *testing.T) {
			_, envelope := mcpModern(t, handler, 1, "tools/call",
				`{"name":"update_service","arguments":{"id":"web","section":"`+
					section+`","value":[]}}`)

			var result toolCallResult
			if err := json.Unmarshal(envelope.Result, &result); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if !result.IsError {
				t.Fatalf("update_service accepted section %q: %s", section, envelope.Result)
			}

			// Refusing is not enough on its own: the section exists, it is
			// just reached by a different tool, and the error is the only
			// place a model finds out which.
			if !strings.Contains(string(envelope.Result), "update_service_"+section) {
				t.Errorf("the error does not name the tool that does this: %s", envelope.Result)
			}
		})
	}
}

// Every section update_service advertises must reach a case in its switch. The
// table that drives them for real would catch one falling through, since it
// asserts the identity survives — but only for the sections it lists.
func TestEveryAdvertisedSectionIsDispatched(t *testing.T) {
	for _, section := range updateServiceSections {
		if _, ok := dispatchedServiceSections[section]; !ok {
			t.Errorf("section %q is advertised but not covered by the dispatch test table", section)
		}
	}

	for section := range dispatchedServiceSections {
		if !slices.Contains(updateServiceSections, section) {
			t.Errorf("section %q is dispatched but not advertised", section)
		}
	}

	// The one pairing still made by hand: a writer decides what an edit does,
	// serviceSectionKeys decides what the result reports about it. A section
	// missing from the projection answers a successful write with an empty
	// details map, which reads as "nothing is set here".
	for _, section := range updateServiceSections {
		if _, ok := serviceSectionKeys[section]; !ok {
			t.Errorf("section %q can be written but has no projection to report it", section)
		}
	}
}

// The shape describe emits and the shape update_service accepts are not the
// same, and Go's decoder matches field names case-insensitively — so a key with
// no field decodes to zero and Docker accepts a port publishing nothing. The
// round trip must fail loudly: an argument carrying an unknown key is rejected.
func TestDecodeSectionRejectsKeysTheSectionHasNoFieldFor(t *testing.T) {
	req := newCallToolRequest("update_service", map[string]any{
		"value": []any{map[string]any{
			"mode": "ingress", "protocol": "tcp", "published": 8099, "target": 80,
		}},
	})

	_, err := decodeSection[[]swarm.PortConfig](req, "ports")
	if err == nil {
		t.Fatal("describe's own port shape decoded silently; it must be refused")
	}

	// Which key it names first does not matter — "mode" is as wrong as
	// "published" — but it has to name one, because the caller's next move is
	// to correct a spelling and a type name alone does not say which.
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error must name the offending key, got %q", err)
	}
}

// A port with no target publishes nothing. Docker accepts it and assigns an
// ephemeral published port pointing at container port 0, so the service ends
// up with a port config that cannot serve — the shape of the corruption above,
// reachable by any caller who simply omits the field.
func TestUpdateServicePortsRejectsAPortWithNoTarget(t *testing.T) {
	req := newCallToolRequest("update_service", map[string]any{
		"value": []any{map[string]any{"Protocol": "tcp", "PublishMode": "ingress"}},
	})

	ports, err := decodeSection[[]swarm.PortConfig](req, "ports")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if err := validatePorts(ports); err == nil {
		t.Error("a port with TargetPort 0 was accepted")
	}
}
