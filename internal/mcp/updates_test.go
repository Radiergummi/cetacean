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
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsImpactful).Handler()

	calls := map[string]string{
		"update_service_resources": `{"id":"web","resources":{"Limits":{"NanoCPUs":1000000000}}}`,
		"update_service_placement": `{"id":"web","placement":{"Constraints":["node.role==worker"]}}`,
		"update_service_ports":     `{"id":"web","ports":[{"TargetPort":80}]}`,
		"update_service_labels":    `{"id":"web","labels":{"tier":"edge"}}`,
		"update_service_env":       `{"id":"web","env":{"DATABASE_PASSWORD":"hunter2"}}`,
		"update_service_log_driver": `{"id":"web",` +
			`"driver":{"Name":"splunk","Options":{"splunk-token":"tok-secret"}}}`,
	}

	for tool, args := range calls {
		t.Run(tool, func(t *testing.T) {
			result := callTool(t, handler, `{"name":"`+tool+`","arguments":`+args+`}`)

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
		`{"name":"update_service_resources","arguments":`+
			`{"id":"web","resources":{"Limits":{"NanoCPUs":2000000000}}}}`)

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
		`{"name":"update_service_env","arguments":{"id":"web","env":{"A":"1"}}}`)

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

	result := callTool(t, handler,
		`{"name":"update_node_availability","arguments":{"id":"node1","availability":"drain"}}`)

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
