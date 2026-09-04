package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// promptListing is the prompts/list result.
type promptListing struct {
	Prompts []struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Arguments   []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		} `json:"arguments"`
	} `json:"prompts"`
}

// listPrompts drives prompts/list through the real transport.
func listPrompts(t *testing.T, handler http.Handler) promptListing {
	t.Helper()

	_, envelope := mcpModern(t, handler, 1, "prompts/list", `{}`)
	if envelope.Error != nil {
		t.Fatalf("prompts/list failed: %+v", envelope.Error)
	}

	var listing promptListing
	if err := json.Unmarshal(envelope.Result, &listing); err != nil {
		t.Fatalf("decode prompts/list: %v (raw %s)", err, envelope.Result)
	}

	return listing
}

// promptNames returns the listed prompt names, for asserting by name rather
// than by count — a prompt moving tier should fail visibly.
func promptNames(listing promptListing) []string {
	names := make([]string, 0, len(listing.Prompts))
	for _, prompt := range listing.Prompts {
		names = append(names, prompt.Name)
	}

	return names
}

// TestPromptsAreRegisteredAndReachable guards registration and end-to-end
// reachability. A unit test on promptCatalog() passes happily whether or not
// registerPrompts() ever ran on a real server; this drives the real transport
// so a missing registerPrompts() call, an empty catalog, or a broken handler
// fails here instead of silently serving no prompts.
func TestPromptsAreRegisteredAndReachable(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	listing := listPrompts(t, handler)
	if len(listing.Prompts) == 0 {
		t.Fatal("prompts/list returned nothing; prompts are not registered")
	}

	_, envelope := mcpModern(t, handler, 2, "prompts/get",
		`{"name":"review_capacity","arguments":{}}`)
	if envelope.Error != nil {
		t.Fatalf("prompts/get failed: %+v", envelope.Error)
	}
}

// TestPromptsListIsTierGated — at tier 0 only the diagnostic prompts exist.
// Asserted by name so a prompt changing tier fails here rather than silently
// appearing in a read-only deployment.
func TestPromptsListIsTierGated(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	got := promptNames(listPrompts(t, handler))
	want := map[string]bool{
		"diagnose_service":      true,
		"explain_unschedulable": true,
		"review_capacity":       true,
	}

	if len(got) != len(want) {
		t.Fatalf("tier 0 listed %v, want exactly %d diagnostic prompts", got, len(want))
	}

	for _, name := range got {
		if !want[name] {
			t.Errorf("tier 0 listed %q, which is not a tier-0 prompt", name)
		}
	}
}

// TestPromptsGetExpandsThroughTheTransport confirms the handler is reachable
// and its argument survives JSON round-tripping.
func TestPromptsGetExpandsThroughTheTransport(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	_, envelope := mcpModern(t, handler, 1, "prompts/get",
		`{"name":"diagnose_service","arguments":{"service":"web"}}`)
	if envelope.Error != nil {
		t.Fatalf("prompts/get failed: %+v", envelope.Error)
	}

	var result struct {
		Messages []struct {
			Role    string `json:"role"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode prompts/get: %v (raw %s)", err, envelope.Result)
	}

	if len(result.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(result.Messages))
	}

	if result.Messages[0].Role != "user" {
		t.Errorf("role = %q, want user", result.Messages[0].Role)
	}

	if want := "web"; !strings.Contains(result.Messages[0].Content.Text, want) {
		t.Errorf("expanded text does not name %q", want)
	}
}

// TestPromptsGetRejectsAMissingArgument — the handler's guard has to survive
// the transport, since mcp-go does not enforce Required itself.
func TestPromptsGetRejectsAMissingArgument(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	_, envelope := mcpModern(t, handler, 1, "prompts/get",
		`{"name":"diagnose_service","arguments":{}}`)
	if envelope.Error == nil {
		t.Fatal("prompts/get with no argument succeeded; the service name is required")
	}
}

// newPromptTestServer wires a server at the given operations level.
func newPromptTestServer(t *testing.T, opsLevel config.OperationsLevel) *Server {
	t.Helper()

	return newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, opsLevel)
}

// listPromptsAs drives prompts/list with an identity on the request context,
// which is how the ACL filter sees a caller.
func listPromptsAs(t *testing.T, srv *Server, identity *auth.Identity) promptListing {
	t.Helper()

	request := modernRequest(t, 1, "prompts/list", `{}`)
	request = request.WithContext(auth.ContextWithIdentity(request.Context(), identity))

	_, envelope := sendMCP(t, srv.Handler(), request)
	if envelope.Error != nil {
		t.Fatalf("prompts/list failed: %+v", envelope.Error)
	}

	var listing promptListing
	if err := json.Unmarshal(envelope.Result, &listing); err != nil {
		t.Fatalf("decode prompts/list: %v (raw %s)", err, envelope.Result)
	}

	return listing
}

// TestPromptsHiddenWhenADrivenToolIsDenied — diagnose_service walks get_logs,
// which needs service:read. A caller with only node grants cannot complete the
// sequence, so it must not be offered.
func TestPromptsHiddenWhenADrivenToolIsDenied(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("node:*"))

	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsImpactful,
		func(o *Options) { o.ACL = evaluator },
	)

	got := promptNames(listPromptsAs(t, srv, &auth.Identity{Subject: "tester"}))
	for _, name := range got {
		if name == "diagnose_service" {
			t.Error("diagnose_service offered to a caller with no service:read grant")
		}

		if name == "drain_node" {
			t.Error("drain_node offered to a caller with only node:read, not node:write")
		}
	}
}

// TestPromptsVisibleWhenEveryDrivenToolIs — the same caller with service:read
// gets the diagnostic prompts and still not the remediation ones, which need
// write.
func TestPromptsVisibleWhenEveryDrivenToolIs(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:*"))

	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsImpactful,
		func(o *Options) { o.ACL = evaluator },
	)

	got := promptNames(listPromptsAs(t, srv, &auth.Identity{Subject: "tester"}))

	found := make(map[string]bool, len(got))
	for _, name := range got {
		found[name] = true
	}

	if !found["diagnose_service"] {
		t.Error("service:read must reveal diagnose_service")
	}

	if found["roll_back_service"] {
		t.Error("service:read must not reveal roll_back_service, which writes")
	}
}

// TestPromptsHiddenEntirelyWithoutGrants is the floor. A caller matching no
// grant can read nothing, so there is no investigation to seed — even though
// the cross-type read tools stay visible in tools/list on their own reasoning.
func TestPromptsHiddenEntirelyWithoutGrants(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:*"},
		Audience:    []string{"user:somebody-else"},
		Permissions: []string{"read"},
	}}})

	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsImpactful,
		func(o *Options) { o.ACL = evaluator },
	)

	if got := promptNames(listPromptsAs(t, srv, &auth.Identity{Subject: "tester"})); len(got) != 0 {
		t.Errorf("a zero-grant caller was offered %v, want nothing", got)
	}
}

// TestPromptsGetOnAHiddenPromptSaysNotFound is a disclosure property. mcp-go
// enforces the filter at get time too and returns the same error as an unknown
// name, so a hidden prompt cannot be confirmed by guessing it. Inherited rather
// than built, and pinned because it is security-relevant.
func TestPromptsGetOnAHiddenPromptSaysNotFound(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("node:*"))

	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsImpactful,
		func(o *Options) { o.ACL = evaluator },
	)

	request := modernRequest(t, 1, "prompts/get",
		`{"name":"diagnose_service","arguments":{"service":"web"}}`)
	request = request.WithContext(
		auth.ContextWithIdentity(request.Context(), &auth.Identity{Subject: "tester"}),
	)

	_, envelope := sendMCP(t, srv.Handler(), request)
	if envelope.Error == nil {
		t.Fatal("prompts/get returned a prompt the caller may not see")
	}

	if !strings.Contains(envelope.Error.Message, "not found") {
		t.Errorf("error was %q; a hidden prompt must be indistinguishable from an unknown one",
			envelope.Error.Message)
	}
}

// TestPromptsListAtEachTier is what makes the gating observable: every tier
// above 0 adds exactly one remediation prompt.
func TestPromptsListAtEachTier(t *testing.T) {
	for _, testCase := range []struct {
		level config.OperationsLevel
		want  []string
	}{
		{
			level: config.OpsReadOnly,
			want:  []string{"diagnose_service", "explain_unschedulable", "review_capacity"},
		},
		{
			level: config.OpsOperational,
			want: []string{
				"diagnose_service", "explain_unschedulable", "review_capacity",
				"roll_back_service",
			},
		},
		{
			level: config.OpsConfiguration,
			want: []string{
				"diagnose_service", "explain_unschedulable", "review_capacity",
				"roll_back_service", "right_size_service",
			},
		},
		{
			level: config.OpsImpactful,
			want: []string{
				"diagnose_service", "explain_unschedulable", "review_capacity",
				"roll_back_service", "right_size_service", "drain_node",
			},
		},
	} {
		handler := newPromptTestServer(t, testCase.level).Handler()

		got := promptNames(listPrompts(t, handler))
		if len(got) != len(testCase.want) {
			t.Errorf("tier %v listed %v, want %v", testCase.level, got, testCase.want)

			continue
		}

		listed := make(map[string]bool, len(got))
		for _, name := range got {
			listed[name] = true
		}

		for _, name := range testCase.want {
			if !listed[name] {
				t.Errorf("tier %v did not list %q", testCase.level, name)
			}
		}
	}
}
