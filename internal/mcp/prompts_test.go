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
		"roll_back_service":     config.OpsOperational,
		"right_size_service":    config.OpsConfiguration,
		"drain_node":            config.OpsImpactful,
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

	if len(want) != len(promptCatalog()) {
		t.Errorf("want tracks %d prompts, but promptCatalog() has %d — "+
			"a prompt is silently skipped", len(want), len(promptCatalog()))
	}
}

// TestPromptTierDerivesFromDrives exercises promptTier directly against a
// synthetic tiers map, independent of what the real catalog happens to drive
// today. TestPromptTierIsTheMaxOfItsTools alone cannot catch a broken
// promptTier: every catalogued prompt currently drives only tier-0 tools, so
// a hardcoded config.OpsReadOnly, a minimum instead of a maximum, or a
// first-entry-only implementation would all pass it unchanged.
func TestPromptTierDerivesFromDrives(t *testing.T) {
	tiers := map[string]config.OperationsLevel{
		"low":     config.OpsReadOnly,
		"high":    config.OpsImpactful,
		"middle":  config.OpsConfiguration,
		"another": config.OpsOperational,
	}

	tests := map[string]struct {
		drives []string
		want   config.OperationsLevel
	}{
		// Low tier ordered first, high tier later: a "first entry wins" or a
		// "minimum wins" implementation both fail this, only "maximum" passes.
		"mixed tiers, highest wins regardless of order": {
			drives: []string{"low", "high", "middle"},
			want:   config.OpsImpactful,
		},
		"single tool, its own tier": {
			drives: []string{"another"},
			want:   config.OpsOperational,
		},
		"no tools, read-only": {
			drives: nil,
			want:   config.OpsReadOnly,
		},
		"unknown tool fails closed to the highest tier": {
			drives: []string{"low", "does-not-exist", "middle"},
			want:   config.OpsImpactful,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			def := promptDef{drives: tt.drives}

			if got := promptTier(def, tiers); got != tt.want {
				t.Errorf("promptTier(%v) = %v, want %v", tt.drives, got, tt.want)
			}
		})
	}
}

// TestPromptCatalogDeclaresItsArguments — the argument names are what #170's
// completion provider will key off, so they are part of the contract.
func TestPromptCatalogDeclaresItsArguments(t *testing.T) {
	want := map[string]string{
		"diagnose_service":      "service",
		"explain_unschedulable": "service",
		"review_capacity":       "",
		"roll_back_service":     "service",
		"right_size_service":    "service",
		"drain_node":            "node",
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

	if len(want) != len(promptCatalog()) {
		t.Errorf("want tracks %d prompts, but promptCatalog() has %d — "+
			"a prompt is silently skipped", len(want), len(promptCatalog()))
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

// TestAllPromptsNameTheirDrivenTools generalizes
// TestDiagnoseServiceExpandsItsArgument's driven-tool assertion to every
// prompt in the catalog, so text rule 1 (name the driven tools explicitly)
// holds for all six, not just diagnose_service.
func TestAllPromptsNameTheirDrivenTools(t *testing.T) {
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

		for _, tool := range def.drives {
			if !strings.Contains(text.Text, tool) {
				t.Errorf("prompt %q expanded text does not name the %q tool it drives",
					def.prompt.Name, tool)
			}
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

// TestRemediationPromptsConfirmBeforeActing is rule 3 of the text
// conventions, and the reason remediation prompts are safe to ship: a runbook
// that assumes its own diagnosis turns "roll this back" into an outage when
// the service was healthy all along.
func TestRemediationPromptsConfirmBeforeActing(t *testing.T) {
	for name, mutation := range map[string]string{
		"roll_back_service":  "rollback_service",
		"right_size_service": "update_service_resources",
		"drain_node":         "update_node_availability",
	} {
		def := findPrompt(t, name)

		result, err := def.handler(context.Background(), getPromptRequest(
			name,
			map[string]string{"service": "example", "node": "example"},
		))
		if err != nil {
			t.Fatalf("prompt %q handler: %v", name, err)
		}

		text, ok := result.Messages[0].Content.(mcplib.TextContent)
		if !ok {
			t.Fatalf("prompt %q content is %T", name, result.Messages[0].Content)
		}

		if !strings.Contains(text.Text, mutation) {
			t.Errorf("prompt %q never names the %q tool it exists to drive", name, mutation)
		}

		// The text must offer an exit that makes no change. Without one, the
		// model has no instruction for "the problem is not real".
		if !strings.Contains(text.Text, "stop") && !strings.Contains(text.Text, "no change") {
			t.Errorf("prompt %q gives the model no way to stop without acting", name)
		}
	}
}
