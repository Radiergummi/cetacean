package mcp

import (
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// recommendationFix names the tool call that applies a finding's remedy.
//
// recommendations.Recommendation carries FixAction as a REST route —
// "PUT /nodes/{id}/availability" — because the dashboard reads it that way and
// dispatches on it (frontend/src/lib/applyRecommendation.ts). An MCP caller
// holds tools, not routes, and cannot issue an HTTP request at all, so the one
// field meant to say what to do next says something it cannot act on.
//
// Translating rather than renaming keeps the REST contract intact: this is the
// only consumer that needs the other vocabulary.
type recommendationFix struct {
	// Tool is the MCP tool to call.
	Tool string `json:"tool"`

	// Section is the tool's `section` argument, for the two folded editors
	// where one tool covers many fields. Empty for a tool that takes none.
	Section string `json:"section,omitempty"`
}

// mcpRecommendation is a finding as MCP serves it.
//
// Recommendation is embedded so every field it grows arrives here without
// this type being touched. FixAction is redeclared at the shallower depth and
// left nil, which is how encoding/json is told to drop it: leaving the route
// beside the tool would let a model pick the one it cannot use.
type mcpRecommendation struct {
	recommendations.Recommendation

	FixAction *string            `json:"fixAction,omitempty"`
	Fix       *recommendationFix `json:"fix,omitempty"`
}

// fixToolsByRoute maps each REST remedy the engine emits to the tool that
// performs it. A route with no entry yields no fix at all rather than a
// passed-through path, so adding a remedy without teaching this table degrades
// to silence instead of reintroducing the defect.
var fixToolsByRoute = map[string]recommendationFix{
	"PUT /services/{id}/scale":       {Tool: "scale_service"},
	"PUT /nodes/{id}/availability":   {Tool: "update_node", Section: "availability"},
	"PATCH /services/{id}/resources": {Tool: "update_service", Section: "resources"},
}

// projectRecommendations restates each finding's remedy as a tool call.
func projectRecommendations(items []recommendations.Recommendation) []mcpRecommendation {
	projected := make([]mcpRecommendation, 0, len(items))

	for _, item := range items {
		entry := mcpRecommendation{Recommendation: item}

		if item.FixAction != nil {
			if fix, ok := fixToolsByRoute[*item.FixAction]; ok {
				entry.Fix = &fix
			}
		}

		projected = append(projected, entry)
	}

	return projected
}

// baseRecommendations unwraps the projection so the engine's own counting rule
// stays the only one. Restating it here would let the summary and the findings
// disagree about what a severity means.
func baseRecommendations(items []mcpRecommendation) []recommendations.Recommendation {
	base := make([]recommendations.Recommendation, 0, len(items))
	for _, item := range items {
		base = append(base, item.Recommendation)
	}

	return base
}
