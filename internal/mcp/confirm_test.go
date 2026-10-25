package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// elicitingContext is what a 2026-07-28 request declaring capabilities
// leaves on the handler's context.
func elicitingContext(
	ctx context.Context,
	capability *mcplib.ElicitationCapability,
) context.Context {
	return mcpserver.WithRequestProtocolInfo(ctx, &mcpserver.RequestProtocolInfo{
		Modern:             true,
		ProtocolVersion:    mcplib.LATEST_PROTOCOL_VERSION,
		ClientCapabilities: &mcplib.ClientCapabilities{Elicitation: capability},
	})
}

func formContext() context.Context {
	return elicitingContext(context.Background(), &mcplib.ElicitationCapability{})
}

func identityContext(ctx context.Context, subject string) context.Context {
	return auth.ContextWithIdentity(ctx, &auth.Identity{Subject: subject, Provider: "headers"})
}

// answer turns req into the retry a client sends after a person responded.
func answer(
	req mcplib.CallToolRequest,
	state string,
	action mcplib.ElicitationResponseAction,
	content any,
) mcplib.CallToolRequest {
	req.Params.RequestState = state
	req.Params.InputResponses = mcplib.InputResponses{
		req.Params.Name: mcplib.NewElicitationInputResponse(
			mcplib.ElicitationResult{Action: action, Content: content},
		),
	}

	return req
}

func accepted(req mcplib.CallToolRequest, state string) mcplib.CallToolRequest {
	return answer(req, state, mcplib.ElicitationResponseActionAccept,
		map[string]any{confirmField: true})
}

// mustAsk asserts the handler answered input_required and returns the result.
func mustAsk(t *testing.T, err error) *mcplib.CallToolResult {
	t.Helper()

	result, ok := asToolOutcome(err)
	if !ok || !result.NeedsInput() {
		t.Fatalf("want an input_required outcome, got %v", err)
	}

	return result
}

type removalFixture struct {
	srv     *Server
	cache   *cache.Cache
	removed []string
}

func newRemovalFixture(t *testing.T, overrides ...func(*Options)) *removalFixture {
	t.Helper()

	f := &removalFixture{cache: cache.New(nil)}
	f.cache.SetService(swarm.Service{
		ID:      "svc1",
		Version: swarm.Version{Index: 7},
		Spec:    swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "shop_web"}},
	})
	f.cache.SetService(swarm.Service{
		ID:      "svc2",
		Version: swarm.Version{Index: 3},
		Spec:    swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "shop_db"}},
	})

	for _, id := range []string{"t1", "t2", "t3"} {
		f.cache.SetTask(swarm.Task{
			ID:           id,
			ServiceID:    "svc1",
			DesiredState: swarm.TaskStateRunning,
			Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
		})
	}

	wc := &fakeWriteClient{removeServiceFn: func(_ context.Context, id string) error {
		f.removed = append(f.removed, id)

		return nil
	}}
	f.srv = newToolTestServer(t, f.cache, wc, config.OpsImpactful, overrides...)

	return f
}

func (f *removalFixture) call(
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

// verifies is how a retry reads a state: signed here, unexpired, for the
// same call, and issued with the same question.
func verifies(signer *stateSigner, token string, want roundTripState) bool {
	got, ok := signer.open(token)

	return ok && got.sameCall(want) && got.asked(want.Asked, want.Version, want.Bind)
}

func TestStateSigner_RefusesTamperingExpiryAndMismatch(t *testing.T) {
	signer, err := newStateSigner()
	if err != nil {
		t.Fatal(err)
	}

	now := time.Unix(1_700_000_000, 0)
	signer.now = func() time.Time { return now }

	want := roundTripState{Subject: "alice", Tool: "remove_service", Args: "abc", Version: 7}
	token := signer.sign(want, confirmationTTL)

	if !verifies(signer, token, want) {
		t.Fatal("a fresh state for the same call should verify")
	}

	payload, signature, _ := strings.Cut(token, ".")
	flipped := []byte(payload)
	flipped[len(flipped)-2] ^= 1

	other, _ := newStateSigner()

	cases := map[string]struct {
		token string
		want  roundTripState
	}{
		"tampered payload": {string(flipped) + "." + signature, want},
		"tampered signature": {
			payload + "." + strings.Repeat("A", len(signature)),
			want,
		},
		"another key": {other.sign(want, confirmationTTL), want},
		"other subject": {
			token,
			roundTripState{Subject: "bob", Tool: "remove_service", Args: "abc", Version: 7},
		},
		"other args": {
			token,
			roundTripState{Subject: "alice", Tool: "remove_service", Args: "abd", Version: 7},
		},
		"other version": {
			token,
			roundTripState{Subject: "alice", Tool: "remove_service", Args: "abc", Version: 8},
		},
		"other tool": {
			token,
			roundTripState{Subject: "alice", Tool: "remove_secret", Args: "abc", Version: 7},
		},
		"no separator":   {payload, want},
		"empty":          {"", want},
		"garbage base64": {"!!.!!", want},
	}

	for name, tc := range cases {
		if verifies(signer, tc.token, tc.want) {
			t.Errorf("%s: verified", name)
		}
	}

	now = now.Add(confirmationTTL)
	if verifies(signer, token, want) {
		t.Error("a state past its expiry verified")
	}
}

func TestArgumentsDigest_IgnoresKeyOrder(t *testing.T) {
	var a, b map[string]any
	_ = json.Unmarshal([]byte(`{"id":"x","force":true}`), &a)
	_ = json.Unmarshal([]byte(`{"force":true,"id":"x"}`), &b)

	if argumentsDigest(newCallToolRequest("t", a)) != argumentsDigest(newCallToolRequest("t", b)) {
		t.Error("the same arguments in another order hashed differently")
	}

	if argumentsDigest(newCallToolRequest("t", a)) ==
		argumentsDigest(newCallToolRequest("t", map[string]any{"id": "y", "force": true})) {
		t.Error("different arguments hashed alike")
	}
}

func TestClientElicits(t *testing.T) {
	cases := map[string]struct {
		ctx       context.Context
		form, url bool
	}{
		"no protocol info": {context.Background(), false, false},
		"modern without elicitation": {
			elicitingContext(context.Background(), nil), false, false,
		},
		"modern empty object means form": {
			elicitingContext(context.Background(), &mcplib.ElicitationCapability{}), true, false,
		},
		"modern url only": {
			elicitingContext(context.Background(), &mcplib.ElicitationCapability{URL: &struct{}{}}),
			false, true,
		},
		"modern both": {
			elicitingContext(context.Background(), &mcplib.ElicitationCapability{
				Form: &struct{}{}, URL: &struct{}{},
			}),
			true, true,
		},
		"legacy session without an ID": {
			mcpserver.NewMCPServer("t", "1").WithContext(context.Background(), session("")),
			false, false,
		},
	}

	for name, tc := range cases {
		if got := clientElicits(tc.ctx, mcplib.ElicitationModeForm); got != tc.form {
			t.Errorf("%s: form = %v, want %v", name, got, tc.form)
		}

		if got := clientElicits(tc.ctx, mcplib.ElicitationModeURL); got != tc.url {
			t.Errorf("%s: url = %v, want %v", name, got, tc.url)
		}
	}
}

// A client that cannot be asked is served as before: the write goes ahead.
func TestRemoveService_WithoutElicitationRemovesAtOnce(t *testing.T) {
	f := newRemovalFixture(t)

	text, err := f.call(t, context.Background(),
		newCallToolRequest("remove_service", map[string]any{"id": "shop_web"}))
	if err != nil {
		t.Fatalf("remove_service: %v", err)
	}

	if text != `{"removed":true}` || len(f.removed) != 1 {
		t.Fatalf("result %s, removed %v", text, f.removed)
	}
}

func TestRemoveService_AsksThenRemovesOnAcceptance(t *testing.T) {
	f := newRemovalFixture(t)
	ctx := identityContext(formContext(), "alice")
	req := newCallToolRequest("remove_service", map[string]any{"id": "shop_web"})

	_, err := f.call(t, ctx, req)
	asked := mustAsk(t, err)

	if len(f.removed) != 0 {
		t.Fatal("removed before anyone answered")
	}

	question, ok := asked.InputRequests["remove_service"]
	if !ok || question.Method != mcplib.MethodElicitationCreate {
		t.Fatalf("input requests = %+v", asked.InputRequests)
	}

	if question.Elicitation.Mode != mcplib.ElicitationModeForm ||
		!strings.Contains(question.Elicitation.Message, "shop_web") ||
		!strings.Contains(question.Elicitation.Message, "3 running tasks") {
		t.Errorf("question = %+v", question.Elicitation)
	}

	text, err := f.call(t, ctx, accepted(req, asked.RequestState))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}

	if text != `{"removed":true}` || len(f.removed) != 1 || f.removed[0] != "shop_web" {
		t.Fatalf("result %s, removed %v", text, f.removed)
	}
}

func TestRemoveService_RefusalIsNotAnError(t *testing.T) {
	cases := map[string]struct {
		action  mcplib.ElicitationResponseAction
		content any
		word    string
	}{
		"declined":  {mcplib.ElicitationResponseActionDecline, nil, "declined"},
		"cancelled": {mcplib.ElicitationResponseActionCancel, nil, "cancelled"},
		"accepted but no": {
			mcplib.ElicitationResponseActionAccept,
			map[string]any{confirmField: false},
			"declined",
		},
		"accepted, string": {
			mcplib.ElicitationResponseActionAccept,
			map[string]any{confirmField: "true"},
			"declined",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRemovalFixture(t)
			req := newCallToolRequest("remove_service", map[string]any{"id": "shop_web"})

			_, err := f.call(t, formContext(), req)
			asked := mustAsk(t, err)

			_, err = f.call(
				t,
				formContext(),
				answer(req, asked.RequestState, tc.action, tc.content),
			)

			result, ok := asToolOutcome(err)
			if !ok || result.NeedsInput() {
				t.Fatalf("want a final refusal, got %v", err)
			}

			if result.IsError {
				t.Error("a refusal was reported as an error")
			}

			text := result.Content[0].(mcplib.TextContent).Text
			if !strings.Contains(text, tc.word) {
				t.Errorf("text %q does not say %q", text, tc.word)
			}

			if got, _ := json.Marshal(
				result.StructuredContent,
			); string(
				got,
			) != `{"removed":false}` {
				t.Errorf("structured content = %s", got)
			}

			if len(f.removed) != 0 {
				t.Errorf("removed %v after a refusal", f.removed)
			}
		})
	}
}

// A confirmation answers for one call: anything that makes the retry a
// different call starts the question over rather than failing.
func TestRemoveService_MismatchedRetryAsksAgain(t *testing.T) {
	cases := map[string]func(f *removalFixture, req mcplib.CallToolRequest, state string) (context.Context, mcplib.CallToolRequest){
		"swapped target": func(_ *removalFixture, req mcplib.CallToolRequest, state string) (context.Context, mcplib.CallToolRequest) {
			swapped := newCallToolRequest("remove_service", map[string]any{"id": "shop_db"})

			return identityContext(formContext(), "alice"), accepted(swapped, state)
		},
		"another caller": func(_ *removalFixture, req mcplib.CallToolRequest, state string) (context.Context, mcplib.CallToolRequest) {
			return identityContext(formContext(), "mallory"), accepted(req, state)
		},
		"target changed": func(f *removalFixture, req mcplib.CallToolRequest, state string) (context.Context, mcplib.CallToolRequest) {
			svc, _ := f.cache.GetService("svc1")
			svc.Version.Index++
			f.cache.SetService(svc)

			return identityContext(formContext(), "alice"), accepted(req, state)
		},
		"tampered state": func(_ *removalFixture, req mcplib.CallToolRequest, state string) (context.Context, mcplib.CallToolRequest) {
			return identityContext(formContext(), "alice"), accepted(req, state+"x")
		},
		"no state": func(_ *removalFixture, req mcplib.CallToolRequest, _ string) (context.Context, mcplib.CallToolRequest) {
			return identityContext(formContext(), "alice"), accepted(req, "")
		},
		"no answer": func(_ *removalFixture, req mcplib.CallToolRequest, state string) (context.Context, mcplib.CallToolRequest) {
			req.Params.RequestState = state

			return identityContext(formContext(), "alice"), req
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRemovalFixture(t)
			req := newCallToolRequest("remove_service", map[string]any{"id": "shop_web"})

			_, err := f.call(t, identityContext(formContext(), "alice"), req)
			asked := mustAsk(t, err)

			ctx, retry := mutate(f, req, asked.RequestState)

			_, err = f.call(t, ctx, retry)
			mustAsk(t, err)

			if len(f.removed) != 0 {
				t.Errorf("removed %v on a mismatched retry", f.removed)
			}
		})
	}
}

func TestRemoveService_ExpiredStateAsksAgain(t *testing.T) {
	f := newRemovalFixture(t)
	now := time.Now()
	f.srv.confirmations.now = func() time.Time { return now }
	req := newCallToolRequest("remove_service", map[string]any{"id": "shop_web"})

	_, err := f.call(t, formContext(), req)
	asked := mustAsk(t, err)

	now = now.Add(confirmationTTL + time.Second)

	_, err = f.call(t, formContext(), accepted(req, asked.RequestState))
	mustAsk(t, err)

	if len(f.removed) != 0 {
		t.Errorf("removed %v on an expired confirmation", f.removed)
	}
}

// The checks run again on the retry: a grant withdrawn while the person was
// answering stops the write, whatever they said.
func TestRemoveService_ChecksRunAgainOnRetry(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:*"},
		Audience:    []string{"*"},
		Permissions: []string{"read", "write"},
	}}})

	f := newRemovalFixture(t, func(o *Options) { o.ACL = evaluator })
	ctx := identityContext(formContext(), "alice")
	req := newCallToolRequest("remove_service", map[string]any{"id": "shop_web"})

	_, err := f.call(t, ctx, req)
	asked := mustAsk(t, err)

	evaluator.SetPolicy(readOnlyPolicy("service:*"))

	_, err = f.call(t, ctx, accepted(req, asked.RequestState))
	if _, ok := asToolOutcome(err); ok || err == nil ||
		!strings.Contains(err.Error(), "write access denied") {
		t.Fatalf("want the write refused, got %v", err)
	}

	if len(f.removed) != 0 {
		t.Errorf("removed %v after the grant was withdrawn", f.removed)
	}
}

// The checks run before the question: nobody is asked to confirm what the
// caller could not do, and the question cannot name what they cannot read.
func TestRemoveService_RefusedCallerIsNotAsked(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:shop_db"))

	f := newRemovalFixture(t, func(o *Options) { o.ACL = evaluator })

	_, err := f.call(t, identityContext(formContext(), "alice"),
		newCallToolRequest("remove_service", map[string]any{"id": "shop_web"}))
	if _, ok := asToolOutcome(err); ok || err == nil {
		t.Fatalf("want a refusal before any question, got %v", err)
	}
}

// A tool that may ask a person must answer before any work starts, and mcp-go
// cannot pause a task for input.
func TestElicitingToolsDeclareNoTaskSupport(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsImpactful)

	eliciting := 0

	for _, td := range srv.registeredTools {
		if !td.elicits {
			continue
		}

		eliciting++

		if td.tool.Execution != nil && td.tool.Execution.TaskSupport != "" &&
			td.tool.Execution.TaskSupport != mcplib.TaskSupportForbidden {
			t.Errorf("%s elicits and declares task support %q",
				td.tool.Name, td.tool.Execution.TaskSupport)
		}
	}

	if eliciting == 0 {
		t.Fatal("no tool elicits; the contract holds nothing")
	}
}

// The whole round trip over HTTP, as a 2026-07-28 client drives it.
func TestRemoveService_RoundTripOverHTTP(t *testing.T) {
	f := newRemovalFixture(t)
	handler := f.srv.Handler()

	capabilities := map[string]any{
		mcplib.MetaKeyClientCapabilities: map[string]any{"elicitation": map[string]any{}},
	}

	params, _ := json.Marshal(map[string]any{
		"name":      "remove_service",
		"arguments": map[string]any{"id": "shop_web"},
		"_meta":     capabilities,
	})

	_, envelope := mcpModern(t, handler, 1, "tools/call", string(params))
	if envelope.Error != nil {
		t.Fatalf("first call: %+v", envelope.Error)
	}

	var first struct {
		ResultType    string                     `json:"resultType"`
		InputRequests map[string]json.RawMessage `json:"inputRequests"`
		RequestState  string                     `json:"requestState"`
	}
	if err := json.Unmarshal(envelope.Result, &first); err != nil {
		t.Fatal(err)
	}

	if first.ResultType != string(mcplib.ResultTypeInputRequired) ||
		first.RequestState == "" || first.InputRequests["remove_service"] == nil {
		t.Fatalf("first result = %s", envelope.Result)
	}

	retry, _ := json.Marshal(map[string]any{
		"name":         "remove_service",
		"arguments":    map[string]any{"id": "shop_web"},
		"_meta":        capabilities,
		"requestState": first.RequestState,
		"inputResponses": map[string]any{
			"remove_service": map[string]any{
				"action":  "accept",
				"content": map[string]any{confirmField: true},
			},
		},
	})

	_, envelope = mcpModern(t, handler, 2, "tools/call", string(retry))
	if envelope.Error != nil {
		t.Fatalf("retry: %+v", envelope.Error)
	}

	var final struct {
		ResultType        string          `json:"resultType"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(envelope.Result, &final); err != nil {
		t.Fatal(err)
	}

	if final.ResultType != string(mcplib.ResultTypeComplete) || final.IsError ||
		string(final.StructuredContent) != `{"removed":true}` {
		t.Fatalf("retry result = %s", envelope.Result)
	}

	if len(f.removed) != 1 {
		t.Fatalf("removed = %v", f.removed)
	}
}
