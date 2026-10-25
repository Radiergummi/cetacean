package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

type twinFixture struct {
	srv    *Server
	cache  *cache.Cache
	writes []string
}

func twinNode(id, addr string, labels map[string]string) swarm.Node {
	return swarm.Node{
		ID:      id,
		Version: swarm.Version{Index: 3},
		Spec: swarm.NodeSpec{
			Annotations:  swarm.Annotations{Labels: labels},
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
		},
		Description: swarm.NodeDescription{
			Hostname: "worker",
			Engine:   swarm.EngineDescription{EngineVersion: "28.5.2"},
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady, Addr: addr},
	}
}

// newTwinFixture is a cluster whose two workers share the hostname "worker".
func newTwinFixture(t *testing.T, overrides ...func(*Options)) *twinFixture {
	t.Helper()

	f := &twinFixture{cache: cache.New(nil)}
	f.cache.SetNode(twinNode("nodeA", "10.0.0.1", map[string]string{"zone": "a"}))
	f.cache.SetNode(twinNode("nodeB", "10.0.0.2", map[string]string{"zone": "b"}))

	wc := &fakeWriteClient{
		updateNodeLabelsFn: func(_ context.Context, id string, _ map[string]string) (swarm.Node, error) {
			f.writes = append(f.writes, "labels:"+id)
			n, _ := f.cache.GetNode(id)
			return n, nil
		},
		updateNodeRoleFn: func(_ context.Context, id string, r swarm.NodeRole) (swarm.Node, error) {
			f.writes = append(f.writes, "role:"+id)
			n, _ := f.cache.GetNode(id)
			n.Spec.Role = r
			return n, nil
		},
	}
	f.srv = newToolTestServer(t, f.cache, wc, config.OpsImpactful, overrides...)

	return f
}

func (f *twinFixture) call(
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

func labelRequest() mcplib.CallToolRequest {
	return newCallToolRequest("update_node_labels",
		map[string]any{"id": "worker", "labels": map[string]any{"tier": "web"}})
}

// pick answers the node question, under its own key.
func pick(req mcplib.CallToolRequest, state, id string) mcplib.CallToolRequest {
	return answerNode(req, state, mcplib.ElicitationResponseActionAccept,
		map[string]any{nodeChoiceKey: id})
}

func answerNode(
	req mcplib.CallToolRequest,
	state string,
	action mcplib.ElicitationResponseAction,
	content any,
) mcplib.CallToolRequest {
	req.Params.RequestState = state
	req.Params.InputResponses = mcplib.InputResponses{
		nodeChoiceKey: mcplib.NewElicitationInputResponse(mcplib.ElicitationResult{
			Action:  action,
			Content: content,
		}),
	}

	return req
}

func TestAmbiguousHostname_AsksWhichNode(t *testing.T) {
	f := newTwinFixture(t)
	req := labelRequest()

	_, err := f.call(t, formContext(), req)
	asked := mustAsk(t, err)

	question, ok := asked.InputRequests[nodeChoiceKey]
	if !ok {
		t.Fatalf("input requests = %+v", asked.InputRequests)
	}

	if !strings.Contains(question.Elicitation.Message, `"worker" names 2 nodes`) {
		t.Errorf("message = %q", question.Elicitation.Message)
	}

	schema := question.Elicitation.RequestedSchema.(map[string]any)
	property := schema["properties"].(map[string]any)[nodeChoiceKey].(map[string]any)
	choices := property["oneOf"].([]map[string]any)

	if len(choices) != 2 || choices[0]["const"] != "nodeA" {
		t.Fatalf("oneOf = %v", choices)
	}

	title := choices[0]["title"].(string)
	for _, want := range []string{"nodeA", "10.0.0.1", "worker, active", "Engine 28.5.2", "zone=a"} {
		if !strings.Contains(title, want) {
			t.Errorf("title %q lacks %q", title, want)
		}
	}

	if _, err := f.call(t, formContext(), pick(req, asked.RequestState, "nodeB")); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if len(f.writes) != 1 || f.writes[0] != "labels:nodeB" {
		t.Errorf("writes = %v, want the chosen node", f.writes)
	}
}

func TestAmbiguousHostname_WithoutElicitationKeepsTheError(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources: []string{
				"node:*",
			},
			Audience:    []string{"*"},
			Permissions: []string{"read", "write"},
		},
	}})

	f := newTwinFixture(t, func(o *Options) { o.ACL = evaluator })

	_, err := f.call(t, identityContext(context.Background(), "alice"), labelRequest())
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want the ambiguity error, got %v", err)
	}
}

// Nobody is shown candidates for a hostname they can't read (#454).
func TestAmbiguousHostname_UnreadableIsNotFoundAndNotAsked(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:*"))

	f := newTwinFixture(t, func(o *Options) { o.ACL = evaluator })

	_, err := f.call(t, identityContext(formContext(), "alice"), labelRequest())
	if _, asked := asToolOutcome(err); asked || err == nil ||
		!strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "nodeA") {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestAmbiguousHostname_ChangedCandidatesAskAgain(t *testing.T) {
	f := newTwinFixture(t)
	req := labelRequest()

	_, err := f.call(t, formContext(), req)
	asked := mustAsk(t, err)

	f.cache.SetNode(twinNode("nodeC", "10.0.0.3", nil))

	_, err = f.call(t, formContext(), pick(req, asked.RequestState, "nodeA"))
	again := mustAsk(t, err)

	choices := again.InputRequests[nodeChoiceKey].Elicitation.RequestedSchema.(map[string]any)["properties"].(map[string]any)[nodeChoiceKey].(map[string]any)["oneOf"].([]map[string]any)
	if len(choices) != 3 {
		t.Errorf("asked again with %d candidates, want 3", len(choices))
	}

	if len(f.writes) != 0 {
		t.Errorf("writes = %v", f.writes)
	}
}

func TestAmbiguousHostname_DeclinedChoiceWritesNothing(t *testing.T) {
	f := newTwinFixture(t)
	req := labelRequest()

	_, err := f.call(t, formContext(), req)
	asked := mustAsk(t, err)

	_, err = f.call(t, formContext(),
		answerNode(req, asked.RequestState, mcplib.ElicitationResponseActionDecline, nil))
	if !errors.Is(err, errNotChosen) {
		t.Fatalf("want errNotChosen, got %v", err)
	}

	if len(f.writes) != 0 {
		t.Errorf("writes = %v", f.writes)
	}
}

// update_node asks which node, then whether to go ahead: the choice made in the
// first round has to survive into the third.
func TestAmbiguousHostname_ChoiceThenConfirmation(t *testing.T) {
	f := newTwinFixture(t)
	req := newCallToolRequest("update_node",
		map[string]any{"id": "worker", "section": "role", "value": "manager"})

	_, err := f.call(t, formContext(), req)
	which := mustAsk(t, err)

	if _, ok := which.InputRequests[nodeChoiceKey]; !ok {
		t.Fatalf("first question = %+v", which.InputRequests)
	}

	_, err = f.call(t, formContext(), pick(req, which.RequestState, "nodeB"))
	confirm := mustAsk(t, err)

	question, ok := confirm.InputRequests["update_node"]
	if !ok || !strings.Contains(question.Elicitation.Message, "Promote node worker to manager.") {
		t.Fatalf("second question = %+v", confirm.InputRequests)
	}

	if _, err := f.call(t, formContext(), accepted(req, confirm.RequestState)); err != nil {
		t.Fatalf("third round: %v", err)
	}

	if len(f.writes) != 1 || f.writes[0] != "role:nodeB" {
		t.Errorf("writes = %v, want role:nodeB", f.writes)
	}
}

func TestAmbiguousHostname_TopologyAsksToo(t *testing.T) {
	f := newTwinFixture(t)

	_, err := f.call(t, formContext(), newCallToolRequest("get_topology",
		map[string]any{"view": "drain-impact", "node": "worker"}))
	mustAsk(t, err)
}
