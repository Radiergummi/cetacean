package mcp

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

func completionCache(t *testing.T) *cache.Cache {
	t.Helper()

	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc-web",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "public-web"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc-db",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "private-db"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node-1",
		Description: swarm.NodeDescription{Hostname: "styx"},
	})

	return c
}

// A client offering cetacean://services/{id} has nothing to put in the blank
// unless the server completes it. The values it offers must be the ones a read
// then resolves, which is why they are names rather than IDs.
func TestCompleteResourceArgumentOffersNames(t *testing.T) {
	srv := newResourceTestServer(t, completionCache(t))

	completion, err := srv.CompleteResourceArgument(
		context.Background(),
		"cetacean://services/{id}",
		mcplib.CompleteArgument{Name: "id", Value: ""},
		mcplib.CompleteContext{},
	)
	if err != nil {
		t.Fatalf("CompleteResourceArgument: %v", err)
	}

	for _, want := range []string{"public-web", "private-db"} {
		if !slices.Contains(completion.Values, want) {
			t.Errorf("completions %v missing %q", completion.Values, want)
		}
	}
}

// Typing narrows the list; a completion that ignored what was typed would
// hand a client the whole cluster to filter itself.
func TestCompleteResourceArgumentFiltersByWhatIsTyped(t *testing.T) {
	srv := newResourceTestServer(t, completionCache(t))

	completion, err := srv.CompleteResourceArgument(
		context.Background(),
		"cetacean://services/{id}",
		mcplib.CompleteArgument{Name: "id", Value: "pub"},
		mcplib.CompleteContext{},
	)
	if err != nil {
		t.Fatalf("CompleteResourceArgument: %v", err)
	}

	if !slices.Equal(completion.Values, []string{"public-web"}) {
		t.Errorf("completions = %v, want only public-web", completion.Values)
	}
}

// The disclosure rule: completion reads the same ACL-filtered listing every
// other read goes through, so it cannot become a way to enumerate resources
// the caller is not allowed to see.
func TestCompleteResourceArgumentHidesWhatACLHides(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:public-*"},
		Audience:    []string{"user:agent@example.com"},
		Permissions: []string{"read"},
	}}})

	srv := newResourceTestServer(t, completionCache(t), func(o *Options) {
		o.ACL = evaluator
	})

	ctx := auth.ContextWithIdentity(
		context.Background(),
		&auth.Identity{Subject: "agent@example.com"},
	)

	completion, err := srv.CompleteResourceArgument(
		ctx,
		"cetacean://services/{id}",
		mcplib.CompleteArgument{Name: "id", Value: ""},
		mcplib.CompleteContext{},
	)
	if err != nil {
		t.Fatalf("CompleteResourceArgument: %v", err)
	}

	if !slices.Contains(completion.Values, "public-web") {
		t.Errorf("completions %v missing the permitted service", completion.Values)
	}

	if slices.Contains(completion.Values, "private-db") {
		t.Errorf("ACL-denied service leaked into completions: %v", completion.Values)
	}
}

// A prompt argument is where completion is most visible: the first thing an
// investigation asks for is the service it is about.
func TestCompletePromptArgumentOffersResourceNames(t *testing.T) {
	srv := newResourceTestServer(t, completionCache(t))

	cases := []struct {
		prompt   string
		argument string
		want     string
	}{
		{"diagnose_service", "service", "public-web"},
		{"drain_node", "node", "styx"},
	}

	for _, tc := range cases {
		t.Run(tc.prompt, func(t *testing.T) {
			completion, err := srv.CompletePromptArgument(
				context.Background(),
				tc.prompt,
				mcplib.CompleteArgument{Name: tc.argument, Value: ""},
				mcplib.CompleteContext{},
			)
			if err != nil {
				t.Fatalf("CompletePromptArgument: %v", err)
			}

			if !slices.Contains(completion.Values, tc.want) {
				t.Errorf("completions %v missing %q", completion.Values, tc.want)
			}
		})
	}
}

// An argument naming something that is not a resource must come back empty
// rather than erroring — a client asks about whatever argument the user is
// editing, and a failed request would surface as a broken server.
func TestCompleteUnknownArgumentIsEmptyNotAnError(t *testing.T) {
	srv := newResourceTestServer(t, completionCache(t))

	completion, err := srv.CompletePromptArgument(
		context.Background(),
		"review_capacity",
		mcplib.CompleteArgument{Name: "nonexistent", Value: ""},
		mcplib.CompleteContext{},
	)
	if err != nil {
		t.Fatalf("CompletePromptArgument: %v", err)
	}

	if len(completion.Values) != 0 {
		t.Errorf("completions = %v, want none", completion.Values)
	}
}

// The MCP spec caps a completion response at 100 values, and a cluster holds
// far more tasks than that. Total and HasMore tell the client what it is not
// being shown.
func TestCompletionsAreCappedAndSayThereIsMore(t *testing.T) {
	c := cache.New(nil)
	for i := range 150 {
		c.SetService(swarm.Service{
			ID: "svc-" + string(rune('a'+i%26)) + strings.Repeat("x", i/26+1),
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
				Name: "svc-" + strings.Repeat("x", i+1),
			}},
		})
	}

	srv := newResourceTestServer(t, c)

	completion, err := srv.CompleteResourceArgument(
		context.Background(),
		"cetacean://services/{id}",
		mcplib.CompleteArgument{Name: "id", Value: ""},
		mcplib.CompleteContext{},
	)
	if err != nil {
		t.Fatalf("CompleteResourceArgument: %v", err)
	}

	if len(completion.Values) > maxCompletionValues {
		t.Errorf("returned %d values, want at most %d", len(completion.Values), maxCompletionValues)
	}

	if !completion.HasMore {
		t.Error("HasMore = false, want true when values were dropped")
	}

	if completion.Total < len(completion.Values) {
		t.Errorf(
			"Total = %d, want at least the %d values sent",
			completion.Total,
			len(completion.Values),
		)
	}
}

// The capability is a promise: a client that sees `completions` will send
// completion/complete and expects an answer. Advertising it without the
// providers wired would promise an answer the server has no way to give.
func TestCompletionsCapabilityIsAdvertisedAndAnswers(t *testing.T) {
	srv := newResourceTestServer(t, completionCache(t))
	handler := srv.Handler()

	_, env := mcpModern(t, handler, 1, "completion/complete",
		`{"ref":{"type":"ref/resource","uri":"cetacean://services/{id}"},`+
			`"argument":{"name":"id","value":"pub"}}`)
	if env.Error != nil {
		t.Fatalf("completion/complete returned error: %+v", env.Error)
	}

	if !strings.Contains(string(env.Result), "public-web") {
		t.Errorf("completion result %s does not offer public-web", env.Result)
	}
}
