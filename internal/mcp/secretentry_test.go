package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
)

const secretValue = "hunter2-correct-horse"

type secretFixture struct {
	srv     *Server
	entries *cluster.SecretEntries
	created []swarm.SecretSpec
}

func newSecretFixture(t *testing.T, overrides ...func(*Options)) *secretFixture {
	t.Helper()

	f := &secretFixture{entries: cluster.NewSecretEntries()}

	wc := &fakeWriteClient{
		createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
			f.created = append(f.created, spec)
			return "sec-new", nil
		},
	}

	opts := append([]func(*Options){func(o *Options) {
		o.SecretEntries = f.entries
		o.DashboardURL = "https://cetacean.example/"
	}}, overrides...)
	f.srv = newToolTestServer(t, cache.New(nil), wc, config.OpsConfiguration, opts...)

	return f
}

func (f *secretFixture) call(
	t *testing.T,
	ctx context.Context,
	args map[string]any,
) (string, error) {
	t.Helper()

	td, ok := f.srv.findTool("create_secret")
	if !ok {
		t.Fatal("create_secret is not registered")
	}

	return td.handler(ctx, newCallToolRequest("create_secret", args))
}

func (f *secretFixture) retry(
	t *testing.T,
	ctx context.Context,
	args map[string]any,
	state string,
	action mcplib.ElicitationResponseAction,
) (string, error) {
	t.Helper()

	td, _ := f.srv.findTool("create_secret")

	return td.handler(ctx, answer(newCallToolRequest("create_secret", args), state, action, nil))
}

// tokenCaller is a caller whose identity came from a bearer token, the only
// kind URL mode binds the dashboard to.
func tokenCaller(ctx context.Context, subject string) context.Context {
	return auth.ContextWithIdentity(
		ctx,
		&auth.Identity{Subject: subject, Provider: auth.ProviderToken},
	)
}

func urlContext() context.Context {
	return elicitingContext(context.Background(), &mcplib.ElicitationCapability{URL: &struct{}{}})
}

func secretArgs() map[string]any {
	return map[string]any{
		"name":     "db_password",
		"labels":   map[string]any{"team": "shop"},
		"encoding": "utf8",
	}
}

func TestCreateSecret_URLModeRoundTrip(t *testing.T) {
	f := newSecretFixture(t)
	ctx := tokenCaller(urlContext(), "alice")

	_, err := f.call(t, ctx, secretArgs())
	asked := mustAsk(t, err)

	question := asked.InputRequests["create_secret"].Elicitation
	if question.Mode != mcplib.ElicitationModeURL || question.ElicitationID == "" ||
		question.URL != "https://cetacean.example/secrets/new?elicitation="+question.ElicitationID {
		t.Fatalf("question = %+v", question)
	}

	entry, err := f.entries.For(question.ElicitationID, "alice")
	if err != nil || entry.Name != "db_password" || entry.Labels["team"] != "shop" {
		t.Fatalf("entry = %+v, %v", entry, err)
	}

	// Not entered yet: the same question again, not a second entry.
	_, err = f.retry(
		t,
		ctx,
		secretArgs(),
		asked.RequestState,
		mcplib.ElicitationResponseActionAccept,
	)
	again := mustAsk(t, err)

	if again.InputRequests["create_secret"].Elicitation.ElicitationID != question.ElicitationID {
		t.Error("a retry before the value was entered opened another entry")
	}

	if err := f.entries.Complete(entry.ID, "alice", "sec-from-dashboard"); err != nil {
		t.Fatal(err)
	}

	text, err := f.retry(
		t,
		ctx,
		secretArgs(),
		again.RequestState,
		mcplib.ElicitationResponseActionAccept,
	)
	if err != nil {
		t.Fatalf("final retry: %v", err)
	}

	if text != `{"id":"sec-from-dashboard","name":"db_password","type":"secret"}` {
		t.Errorf("result = %s", text)
	}

	if len(f.created) != 0 {
		t.Errorf("MCP created a secret itself: %+v", f.created)
	}
}

func TestCreateSecret_URLModeDeclineIsNotAnError(t *testing.T) {
	f := newSecretFixture(t)
	ctx := tokenCaller(urlContext(), "alice")

	_, err := f.call(t, ctx, secretArgs())
	asked := mustAsk(t, err)
	id := asked.InputRequests["create_secret"].Elicitation.ElicitationID

	_, err = f.retry(
		t,
		ctx,
		secretArgs(),
		asked.RequestState,
		mcplib.ElicitationResponseActionDecline,
	)

	result, ok := asToolOutcome(err)
	if !ok || result.NeedsInput() || result.IsError {
		t.Fatalf("want a non-error refusal, got %v", err)
	}

	if _, err := f.entries.For(id, "alice"); err == nil {
		t.Error("a declined entry stayed open")
	}
}

func TestCreateSecret_URLCapableClientMayNotPassTheValue(t *testing.T) {
	f := newSecretFixture(t)
	args := secretArgs()
	args["data"] = secretValue

	_, err := f.call(t, tokenCaller(urlContext(), "alice"), args)
	if err == nil || !strings.Contains(err.Error(), "leave it out") {
		t.Fatalf("want the inline value refused, got %v", err)
	}

	if strings.Contains(err.Error(), secretValue) {
		t.Error("the refusal repeats the value")
	}

	if len(f.created) != 0 {
		t.Errorf("created %+v", f.created)
	}
}

// Without a token's subject there is nobody to bind the dashboard to, so the
// caller is served as a client without URL elicitation.
func TestCreateSecret_UnauthenticatedCallerIsNotURLCapable(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"no identity": urlContext(),
		"bypassed provider": auth.ContextWithIdentity(urlContext(),
			&auth.Identity{Subject: "cn=agent", Provider: "cert"}),
	} {
		t.Run(name, func(t *testing.T) {
			f := newSecretFixture(t)
			args := secretArgs()
			args["data"] = secretValue

			text, err := f.call(t, ctx, args)
			if err != nil {
				t.Fatalf("inline create: %v", err)
			}

			if strings.Contains(text, secretValue) {
				t.Error("the result repeats the value")
			}

			if len(f.created) != 1 || string(f.created[0].Data) != secretValue {
				t.Errorf("created %+v", f.created)
			}

			if _, err := f.call(t, ctx, secretArgs()); err == nil ||
				!strings.Contains(err.Error(), "data: required") {
				t.Errorf("want data required, got %v", err)
			}
		})
	}
}

func TestCreateSecret_InlineValuesSetting(t *testing.T) {
	args := secretArgs()
	args["data"] = secretValue

	off := newSecretFixture(t, func(o *Options) { o.Config.InlineSecretValues = false })
	if _, err := off.call(t, context.Background(), args); err == nil ||
		!strings.Contains(err.Error(), "mcp.inline_secret_values") {
		t.Errorf("want a refusal naming the setting, got %v", err)
	}

	if len(off.created) != 0 {
		t.Errorf("created %+v with inline values off", off.created)
	}

	on := newSecretFixture(t)
	if _, err := on.call(t, context.Background(), args); err != nil {
		t.Errorf("inline value refused by default: %v", err)
	}
}

// The value never reaches a result or a question, on either path.
func TestCreateSecret_NeverEchoesTheValue(t *testing.T) {
	f := newSecretFixture(t)

	_, err := f.call(t, tokenCaller(urlContext(), "alice"), secretArgs())
	asked := mustAsk(t, err)

	encoded, _ := json.Marshal(asked)
	if strings.Contains(string(encoded), secretValue) {
		t.Error("the question carries the value")
	}

	args := secretArgs()
	args["data"] = secretValue

	text, err := f.call(t, context.Background(), args)
	if err != nil || strings.Contains(text, secretValue) {
		t.Errorf("result %q, %v", text, err)
	}
}
