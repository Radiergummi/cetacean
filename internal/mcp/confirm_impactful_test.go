package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// impactfulFixture is a cluster with one resource of each type the level-3
// tools act on, each used by a readable and a hidden service.
type impactfulFixture struct {
	srv    *Server
	writes []string
}

func usingService(id, name string) swarm.Service {
	return swarm.Service{
		ID:      id,
		Version: swarm.Version{Index: 4},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Configs: []*swarm.ConfigReference{{ConfigID: "cfg1"}},
					Secrets: []*swarm.SecretReference{{SecretID: "sec1"}},
					Mounts:  []mount.Mount{{Type: mount.TypeVolume, Source: "data"}},
				},
				Networks: []swarm.NetworkAttachmentConfig{{Target: "net1"}},
			},
		},
	}
}

func newImpactfulFixture(t *testing.T, overrides ...func(*Options)) *impactfulFixture {
	t.Helper()

	c := cache.New(nil)
	c.SetService(usingService("svc1", "shop_web"))
	c.SetService(usingService("svc2", "hidden_billing"))
	c.SetConfig(swarm.Config{
		ID:      "cfg1",
		Version: swarm.Version{Index: 2},
		Spec:    swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app_conf"}},
	})
	c.SetSecret(swarm.Secret{
		ID:      "sec1",
		Version: swarm.Version{Index: 2},
		Spec:    swarm.SecretSpec{Annotations: swarm.Annotations{Name: "db_password"}},
	})
	c.SetNetwork(network.Summary{ID: "net1", Name: "backend"})
	c.SetVolume(volume.Volume{Name: "data", CreatedAt: "2026-01-01T00:00:00Z"})
	c.SetNode(swarm.Node{
		ID:      "node1",
		Version: swarm.Version{Index: 9},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityActive,
		},
		Description:   swarm.NodeDescription{Hostname: "manager-1"},
		Status:        swarm.NodeStatus{State: swarm.NodeStateReady},
		ManagerStatus: &swarm.ManagerStatus{Reachability: swarm.ReachabilityReachable},
	})
	c.SetTask(swarm.Task{
		ID:           "task1",
		ServiceID:    "svc1",
		NodeID:       "node1",
		Slot:         1,
		Version:      swarm.Version{Index: 5},
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})
	c.SetTask(swarm.Task{
		ID:           "task2",
		ServiceID:    "svc2",
		NodeID:       "node1",
		Slot:         1,
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	f := &impactfulFixture{}
	record := func(what string) { f.writes = append(f.writes, what) }

	wc := &fakeWriteClient{
		removeTaskFn:    func(_ context.Context, id string) error { record("task:" + id); return nil },
		removeConfigFn:  func(_ context.Context, id string) error { record("config:" + id); return nil },
		removeSecretFn:  func(_ context.Context, id string) error { record("secret:" + id); return nil },
		removeNetworkFn: func(_ context.Context, id string) error { record("network:" + id); return nil },
		removeVolumeFn: func(_ context.Context, name string, _ bool) error {
			record("volume:" + name)
			return nil
		},
		updateNodeAvailFn: func(_ context.Context, id string, a swarm.NodeAvailability) (swarm.Node, error) {
			record("availability:" + string(a))
			n, _ := c.GetNode(id)
			n.Spec.Availability = a
			return n, nil
		},
		updateNodeRoleFn: func(_ context.Context, id string, r swarm.NodeRole) (swarm.Node, error) {
			record("role:" + string(r))
			n, _ := c.GetNode(id)
			n.Spec.Role = r
			return n, nil
		},
	}

	f.srv = newToolTestServer(t, c, wc, config.OpsImpactful, overrides...)

	return f
}

func (f *impactfulFixture) call(
	t *testing.T,
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	t.Helper()

	td, ok := f.srv.findTool(req.Params.Name)
	if !ok {
		t.Fatalf("%s is not registered", req.Params.Name)
	}

	return td.handler(ctx, req)
}

// shopOnly lets the caller read and write everything except the hidden service.
func shopOnly() *acl.Evaluator {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources: []string{
			"service:shop_*", "config:*", "secret:*", "network:*", "volume:*", "node:*",
			"task:*",
		},
		Audience:    []string{"*"},
		Permissions: []string{"read", "write"},
	}}})

	return evaluator
}

var impactfulCalls = []struct {
	tool     string
	args     map[string]any
	write    string
	mentions []string
}{
	{"remove_task", map[string]any{"id": "task1"}, "task:task1",
		[]string{"Remove task shop_web.1", "replacement for service shop_web"}},
	{"remove_config", map[string]any{"id": "app_conf"}, "config:cfg1",
		[]string{"Remove config app_conf.", "shop_web", "1 other service you can't see"}},
	{"remove_secret", map[string]any{"id": "db_password"}, "secret:sec1",
		[]string{"Remove secret db_password.", "can't be recovered"}},
	{"remove_network", map[string]any{"id": "backend"}, "network:net1",
		[]string{"Remove network backend."}},
	{"remove_volume", map[string]any{"name": "data"}, "volume:data",
		[]string{"Remove volume data.", "contents are deleted"}},
	{"update_node", map[string]any{"id": "manager-1", "section": "availability", "value": "drain"},
		"availability:drain", []string{"Drain node manager-1.", "Runs tasks of shop_web"}},
	{"update_node", map[string]any{"id": "manager-1", "section": "availability", "value": "pause"},
		"availability:pause", []string{"Pause node manager-1."}},
	{"update_node", map[string]any{"id": "manager-1", "section": "role", "value": "worker"},
		"role:worker", []string{"Demote node manager-1 to worker.", "no manager"}},
}

func TestImpactfulTools_AskThenWriteOnAcceptance(t *testing.T) {
	for _, tc := range impactfulCalls {
		t.Run(tc.write, func(t *testing.T) {
			f := newImpactfulFixture(t, func(o *Options) { o.ACL = shopOnly() })
			ctx := identityContext(formContext(), "alice")
			req := newCallToolRequest(tc.tool, tc.args)

			_, err := f.call(t, ctx, req)
			asked := mustAsk(t, err)

			if len(f.writes) != 0 {
				t.Fatalf("wrote %v before anyone answered", f.writes)
			}

			message := asked.InputRequests[tc.tool].Elicitation.Message
			for _, want := range tc.mentions {
				if !strings.Contains(message, want) {
					t.Errorf("message lacks %q:\n%s", want, message)
				}
			}

			if strings.Contains(message, "hidden_billing") {
				t.Errorf("message names a service the caller can't read:\n%s", message)
			}

			if _, err := f.call(t, ctx, accepted(req, asked.RequestState)); err != nil {
				t.Fatalf("retry: %v", err)
			}

			if len(f.writes) != 1 || f.writes[0] != tc.write {
				t.Errorf("writes = %v, want [%s]", f.writes, tc.write)
			}
		})
	}
}

func TestImpactfulTools_DeclineWritesNothingAndKeepsTheSchema(t *testing.T) {
	for _, tc := range impactfulCalls {
		t.Run(tc.write, func(t *testing.T) {
			f := newImpactfulFixture(t)
			req := newCallToolRequest(tc.tool, tc.args)

			_, err := f.call(t, formContext(), req)
			asked := mustAsk(t, err)

			_, err = f.call(t, formContext(), answer(req, asked.RequestState,
				mcplib.ElicitationResponseActionDecline, nil))

			result, ok := asToolOutcome(err)
			if !ok || result.NeedsInput() || result.IsError {
				t.Fatalf("want a non-error refusal, got %v", err)
			}

			if len(f.writes) != 0 {
				t.Errorf("wrote %v after a refusal", f.writes)
			}

			structured, _ := json.Marshal(result.StructuredContent)
			if tc.tool == "update_node" {
				if !strings.Contains(string(structured), `"availability":"active"`) &&
					!strings.Contains(string(structured), `"role":"manager"`) {
					t.Errorf("refusal reports %s, want the node as it stands", structured)
				}
			} else if string(structured) != `{"removed":false}` {
				t.Errorf("structured content = %s", structured)
			}
		})
	}
}

// Every tool at the impactful tier asks before it acts.
func TestImpactfulToolsAllElicit(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsImpactful)

	for _, td := range srv.registeredTools {
		if td.tier() == config.OpsImpactful && !td.elicits {
			t.Errorf("%s is impactful but does not ask for a confirmation", td.tool.Name)
		}
	}
}

func TestRequireConfirmation(t *testing.T) {
	require := func(o *Options) { o.Config.RequireConfirmation = true }

	t.Run("refuses a client that can't be asked", func(t *testing.T) {
		f := newImpactfulFixture(t, require)

		_, err := f.call(t, context.Background(),
			newCallToolRequest("remove_secret", map[string]any{"id": "db_password"}))
		if err == nil || !strings.Contains(err.Error(), "mcp.require_confirmation") {
			t.Fatalf("want a refusal naming the setting, got %v", err)
		}

		if len(f.writes) != 0 {
			t.Errorf("wrote %v", f.writes)
		}
	})

	t.Run("still asks a client that can be", func(t *testing.T) {
		f := newImpactfulFixture(t, require)

		_, err := f.call(t, formContext(),
			newCallToolRequest("remove_secret", map[string]any{"id": "db_password"}))
		mustAsk(t, err)
	})

	t.Run("off serves the client as before", func(t *testing.T) {
		f := newImpactfulFixture(t)

		if _, err := f.call(t, context.Background(),
			newCallToolRequest("remove_secret", map[string]any{"id": "db_password"})); err != nil {
			t.Fatal(err)
		}

		if len(f.writes) != 1 {
			t.Errorf("writes = %v", f.writes)
		}
	})

	t.Run("leaves lower tiers alone", func(t *testing.T) {
		f := newImpactfulFixture(t, require)

		_, err := f.call(t, context.Background(), newCallToolRequest("update_node_labels",
			map[string]any{"id": "manager-1", "labels": map[string]any{"a": "b"}}))
		if err != nil && strings.Contains(err.Error(), "require_confirmation") {
			t.Errorf("a level-2 tool was refused: %v", err)
		}
	})
}

// A target swapped for another of the same kind between the question and the
// answer gets a new question, not the write.
func TestUpdateNode_RoleAnswerDoesNotCoverAvailability(t *testing.T) {
	f := newImpactfulFixture(t)
	role := newCallToolRequest("update_node",
		map[string]any{"id": "manager-1", "section": "role", "value": "worker"})

	_, err := f.call(t, formContext(), role)
	asked := mustAsk(t, err)

	drain := newCallToolRequest("update_node",
		map[string]any{"id": "manager-1", "section": "availability", "value": "drain"})

	_, err = f.call(t, formContext(), accepted(drain, asked.RequestState))
	mustAsk(t, err)

	if len(f.writes) != 0 {
		t.Errorf("wrote %v", f.writes)
	}
}
