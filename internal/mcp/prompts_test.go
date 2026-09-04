package mcp

import (
	"context"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// TestPromptCatalogDrivesOnlyRealTools is the guard that makes deriving tier
// and ACL visibility from `drives` safe. A renamed or removed tool would
// otherwise silently mis-tier a prompt (an unknown name contributes no tier)
// or hide it from every caller forever.
func TestPromptCatalogDrivesOnlyRealTools(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))
	tiers := srv.toolTiers()

	for _, def := range promptCatalog() {
		for _, tool := range def.drives {
			if _, ok := tiers[tool]; !ok {
				t.Errorf("prompt %q drives %q, which is not in toolCatalog()",
					def.prompt.Name, tool)
			}
		}
	}
}

// TestPromptTierIsTheMaxOfItsTools pins the derivation rule. Hardcoding a tier
// would let a prompt advertise itself below the tier of a mutation it
// instructs.
func TestPromptTierIsTheMaxOfItsTools(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))
	tiers := srv.toolTiers()

	want := map[string]config.OperationsLevel{
		"diagnose_service":      config.OpsReadOnly,
		"explain_unschedulable": config.OpsReadOnly,
		"review_capacity":       config.OpsReadOnly,
	}

	for _, def := range promptCatalog() {
		expected, tracked := want[def.prompt.Name]
		if !tracked {
			continue
		}

		if got := promptTier(def, tiers); got != expected {
			t.Errorf("prompt %q tier = %v, want %v", def.prompt.Name, got, expected)
		}
	}
}

// TestPromptCatalogDeclaresItsArguments — the argument names are what #170's
// completion provider will key off, so they are part of the contract.
func TestPromptCatalogDeclaresItsArguments(t *testing.T) {
	want := map[string]string{
		"diagnose_service":      "service",
		"explain_unschedulable": "service",
		"review_capacity":       "",
	}

	for _, def := range promptCatalog() {
		expected, tracked := want[def.prompt.Name]
		if !tracked {
			continue
		}

		if expected == "" {
			if len(def.prompt.Arguments) != 0 {
				t.Errorf("prompt %q takes %d arguments, want none",
					def.prompt.Name, len(def.prompt.Arguments))
			}

			continue
		}

		if len(def.prompt.Arguments) != 1 {
			t.Fatalf("prompt %q has %d arguments, want 1",
				def.prompt.Name, len(def.prompt.Arguments))
		}

		arg := def.prompt.Arguments[0]
		if arg.Name != expected {
			t.Errorf("prompt %q argument = %q, want %q", def.prompt.Name, arg.Name, expected)
		}

		if !arg.Required {
			t.Errorf("prompt %q argument %q must be required", def.prompt.Name, arg.Name)
		}
	}
}

// TestDiagnoseServiceExpandsItsArgument is the wording guard: the expanded
// messages are asserted so the text is reviewable and cannot drift silently.
func TestDiagnoseServiceExpandsItsArgument(t *testing.T) {
	def := findPrompt(t, "diagnose_service")

	result, err := def.handler(context.Background(), getPromptRequest(
		"diagnose_service",
		map[string]string{"service": "web"},
	))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if len(result.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(result.Messages))
	}

	message := result.Messages[0]
	if message.Role != mcplib.RoleUser {
		t.Errorf("role = %q, want %q", message.Role, mcplib.RoleUser)
	}

	text, ok := message.Content.(mcplib.TextContent)
	if !ok {
		t.Fatalf("content is %T, want mcplib.TextContent", message.Content)
	}

	if !strings.Contains(text.Text, "web") {
		t.Error("expanded text does not name the service")
	}

	// The sequence is the content: every driven tool must appear by name, or
	// the prompt has told the model nothing the tool list did not.
	for _, tool := range def.drives {
		if !strings.Contains(text.Text, tool) {
			t.Errorf("expanded text does not name the %q tool it drives", tool)
		}
	}
}

// TestPromptHandlerRejectsAMissingArgument — mcp-go does not enforce
// PromptArgument.Required, so a client may omit it. Interpolating an empty
// string would seed a conversation about the service named "".
func TestPromptHandlerRejectsAMissingArgument(t *testing.T) {
	def := findPrompt(t, "diagnose_service")

	for name, args := range map[string]map[string]string{
		"absent": {},
		"empty":  {"service": ""},
		"blank":  {"service": "   "},
	} {
		if _, err := def.handler(
			context.Background(),
			getPromptRequest("diagnose_service", args),
		); err == nil {
			t.Errorf("%s argument: handler returned no error", name)
		}
	}
}

// TestPromptTextMakesNoClusterClaims enforces rule 4 of the text conventions.
// A prompt asserting something false about this cluster teaches the model a
// wrong premise it will then act on, and the text is static so it cannot know.
//
// "there are three" is deliberately not in this list: it is a false-positive
// hazard rather than a guard. Method text ("there are three things to check")
// is legitimate and would fail the suite for no reason. The remaining phrases
// all assert cluster *state*, which is what the rule forbids.
func TestPromptTextMakesNoClusterClaims(t *testing.T) {
	// Phrases that can only be true of a particular cluster.
	banned := []string{
		"your cluster has",
		"this cluster has",
		"the cluster runs",
	}

	for _, def := range promptCatalog() {
		result, err := def.handler(context.Background(), getPromptRequest(
			def.prompt.Name,
			map[string]string{"service": "example", "node": "example"},
		))
		if err != nil {
			t.Fatalf("prompt %q handler: %v", def.prompt.Name, err)
		}

		text, ok := result.Messages[0].Content.(mcplib.TextContent)
		if !ok {
			t.Fatalf("prompt %q content is %T", def.prompt.Name, result.Messages[0].Content)
		}

		lowered := strings.ToLower(text.Text)
		for _, phrase := range banned {
			if strings.Contains(lowered, phrase) {
				t.Errorf("prompt %q asserts %q about this cluster", def.prompt.Name, phrase)
			}
		}
	}
}

// TestPromptTextCarriesNoBackticks — the constants are Go raw strings, which
// are backtick-delimited, so a backtick cannot appear in one. A contributor
// adding markdown code spans would have to break the literal to do it; this
// fails first and says why.
func TestPromptTextCarriesNoBackticks(t *testing.T) {
	for _, def := range promptCatalog() {
		result, err := def.handler(context.Background(), getPromptRequest(
			def.prompt.Name,
			map[string]string{"service": "example", "node": "example"},
		))
		if err != nil {
			t.Fatalf("prompt %q handler: %v", def.prompt.Name, err)
		}

		text, ok := result.Messages[0].Content.(mcplib.TextContent)
		if !ok {
			t.Fatalf("prompt %q content is %T", def.prompt.Name, result.Messages[0].Content)
		}

		if strings.Contains(text.Text, "`") {
			t.Errorf("prompt %q text contains a backtick; the constants are raw strings",
				def.prompt.Name)
		}
	}
}

// findPrompt returns the catalog entry under name.
func findPrompt(t *testing.T, name string) promptDef {
	t.Helper()

	for _, def := range promptCatalog() {
		if def.prompt.Name == name {
			return def
		}
	}

	t.Fatalf("prompt %q is not in promptCatalog()", name)

	return promptDef{}
}

// getPromptRequest builds a prompts/get request for a handler called directly.
func getPromptRequest(name string, args map[string]string) mcplib.GetPromptRequest {
	return mcplib.GetPromptRequest{
		Params: mcplib.GetPromptParams{
			Name:      name,
			Arguments: args,
		},
	}
}
