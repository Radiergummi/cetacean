package mcp

import (
	"context"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/config"
)

// promptDef is a prompt plus the tools it walks. drives is load-bearing: both
// the prompt's operations tier and its per-caller visibility derive from it, so
// a prompt cannot advertise itself below the tier of a mutation it instructs,
// and cannot be offered to a caller who could not complete it.
//
// The order of drives is the order the text walks them, which keeps the
// declaration readable against the prompt body.
type promptDef struct {
	prompt  mcplib.Prompt
	drives  []string
	handler mcpserver.PromptHandlerFunc
}

// promptCatalog is the static set of prompts. A free function rather than a
// Server method because handlers read no cluster data — they interpolate an
// argument into static text, so there is nothing to close over.
func promptCatalog() []promptDef {
	return []promptDef{
		{
			prompt: mcplib.NewPrompt("diagnose_service",
				mcplib.WithPromptTitle("Diagnose an unhealthy service"),
				mcplib.WithPromptDescription(
					"Walk a service's tasks, the failing task's logs, its metrics and "+
						"recent changes to find why it is unhealthy. Reads only.",
				),
				mcplib.WithArgument("service",
					mcplib.ArgumentTitle("Service"),
					mcplib.ArgumentDescription("Name or ID of the unhealthy service"),
					mcplib.RequiredArgument(),
				),
			),
			drives: []string{
				"search",
				"list_resources",
				"get_logs",
				"get_metrics",
				"get_recommendations",
			},
			handler: interpolatingHandler(
				"Diagnose an unhealthy service",
				promptTextDiagnoseService,
				"service",
			),
		},
		{
			prompt: mcplib.NewPrompt("explain_unschedulable",
				mcplib.WithPromptTitle("Explain why a service will not schedule"),
				mcplib.WithPromptDescription(
					"Separate the four causes of an unplaced task: placement constraints, "+
						"node labels, node availability, and resource reservations. Reads only.",
				),
				mcplib.WithArgument("service",
					mcplib.ArgumentTitle("Service"),
					mcplib.ArgumentDescription("Name or ID of the service that will not schedule"),
					mcplib.RequiredArgument(),
				),
			),
			drives: []string{"search", "list_resources"},
			handler: interpolatingHandler(
				"Explain why a service will not schedule",
				promptTextExplainUnschedulable,
				"service",
			),
		},
		{
			prompt: mcplib.NewPrompt("review_capacity",
				mcplib.WithPromptTitle("Review cluster capacity"),
				mcplib.WithPromptDescription(
					"Join node capacity, reservations, real usage and sizing findings to say "+
						"where the cluster is constrained. Reads only.",
				),
			),
			drives: []string{"list_resources", "get_metrics", "get_recommendations"},
			handler: staticHandler(
				"Review cluster capacity",
				promptTextReviewCapacity,
			),
		},
		{
			prompt: mcplib.NewPrompt("roll_back_service",
				mcplib.WithPromptTitle("Roll a service back"),
				mcplib.WithPromptDescription(
					"Confirm a service is actually degraded, then roll it back to its "+
						"previous version and wait for the replicas to run.",
				),
				mcplib.WithArgument("service",
					mcplib.ArgumentTitle("Service"),
					mcplib.ArgumentDescription("Name or ID of the service to roll back"),
					mcplib.RequiredArgument(),
				),
			),
			drives: []string{"search", "list_resources", "rollback_service"},
			handler: interpolatingHandler(
				"Roll a service back",
				promptTextRollBackService,
				"service",
			),
		},
		{
			prompt: mcplib.NewPrompt("right_size_service",
				mcplib.WithPromptTitle("Right-size a service"),
				mcplib.WithPromptDescription(
					"Check a sizing recommendation against measured use, then correct the "+
						"service's reservations. Replaces every task.",
				),
				mcplib.WithArgument("service",
					mcplib.ArgumentTitle("Service"),
					mcplib.ArgumentDescription("Name or ID of the service to right-size"),
					mcplib.RequiredArgument(),
				),
			),
			drives: []string{
				"search",
				"get_metrics",
				"get_recommendations",
				"update_service_resources",
			},
			handler: interpolatingHandler(
				"Right-size a service",
				promptTextRightSizeService,
				"service",
			),
		},
		{
			prompt: mcplib.NewPrompt("drain_node",
				mcplib.WithPromptTitle("Drain a node for maintenance"),
				mcplib.WithPromptDescription(
					"Check quorum and that the node's work can be placed elsewhere, then "+
						"drain it and confirm the tasks moved.",
				),
				mcplib.WithArgument("node",
					mcplib.ArgumentTitle("Node"),
					mcplib.ArgumentDescription("Hostname or ID of the node to drain"),
					mcplib.RequiredArgument(),
				),
			),
			drives: []string{"search", "list_resources", "update_node_availability"},
			handler: interpolatingHandler(
				"Drain a node for maintenance",
				promptTextDrainNode,
				"node",
			),
		},
	}
}

// interpolatingHandler builds a handler that substitutes one required argument
// into the text. The argument is not checked against the cache: the text tells
// the model to resolve the name with search first, which is what
// mcpInstructions already asks of every client, and failing prompts/get on a
// typo leaves the client no way to correct it.
func interpolatingHandler(description, text, argument string) mcpserver.PromptHandlerFunc {
	return func(
		_ context.Context,
		req mcplib.GetPromptRequest,
	) (*mcplib.GetPromptResult, error) {
		value, err := requiredArgument(req, argument)
		if err != nil {
			return nil, err
		}

		return promptResult(description, fmt.Sprintf(text, value)), nil
	}
}

// staticHandler builds a handler for a prompt that takes no arguments.
func staticHandler(description, text string) mcpserver.PromptHandlerFunc {
	return func(
		_ context.Context,
		_ mcplib.GetPromptRequest,
	) (*mcplib.GetPromptResult, error) {
		return promptResult(description, text), nil
	}
}

// promptResult wraps text as the single user message a prompt expands to. One
// message, and never a fabricated assistant turn: the host is seeding a
// conversation, not replaying one.
func promptResult(description, text string) *mcplib.GetPromptResult {
	return mcplib.NewGetPromptResult(description, []mcplib.PromptMessage{
		mcplib.NewPromptMessage(mcplib.RoleUser, mcplib.NewTextContent(text)),
	})
}

// requiredArgument reads an argument a prompt cannot expand without. mcp-go
// does not enforce PromptArgument.Required, so a client may omit it; without
// this the text would interpolate an empty string and seed a conversation
// about the service named "".
func requiredArgument(req mcplib.GetPromptRequest, name string) (string, error) {
	value := strings.TrimSpace(req.Params.Arguments[name])
	if value == "" {
		return "", fmt.Errorf("prompt argument %q is required", name)
	}

	return value, nil
}

// toolTiers maps every tool name to its operations tier, for deriving a
// prompt's tier from the tools it drives.
func (s *Server) toolTiers() map[string]config.OperationsLevel {
	catalog := s.toolCatalog()

	tiers := make(map[string]config.OperationsLevel, len(catalog))
	for _, td := range catalog {
		tiers[td.tool.Name] = td.tier
	}

	return tiers
}

// promptTier is the highest tier among the tools a prompt drives, so a prompt
// is never offered at a tier that would refuse one of its steps.
//
// A name absent from tiers is treated as the highest tier rather than
// contributing nothing: there is nowhere sensible to fail from a pure
// function called during registration, so an unknown name fails closed by
// mis-tiering the prompt loudly — it drops out of every deployment below the
// top tier — instead of under-tiering it into visibility it should not have.
// TestPromptCatalogDrivesOnlyRealTools is the guard that catches a typo at
// authoring time, before it ever reaches this fallback.
func promptTier(def promptDef, tiers map[string]config.OperationsLevel) config.OperationsLevel {
	tier := config.OpsReadOnly
	for _, name := range def.drives {
		got, ok := tiers[name]
		if !ok {
			got = config.OpsImpactful
		}
		if got > tier {
			tier = got
		}
	}

	return tier
}

// registerPrompts registers every prompt the deployment's operations level
// permits. Tier gating happens here, once, exactly as registerTools does it;
// per-caller ACL visibility is a request-time filter.
func (s *Server) registerPrompts() {
	tiers := s.toolTiers()
	opsLevel := s.config.EffectiveOperationsLevel(s.globalOpsLevel)

	catalog := promptCatalog()

	s.registeredPrompts = make([]promptDef, 0, len(catalog))
	for _, def := range catalog {
		if opsLevel < promptTier(def, tiers) {
			continue
		}

		s.registeredPrompts = append(s.registeredPrompts, def)
		s.mcpServer.AddPrompt(def.prompt, def.handler)
	}
}
