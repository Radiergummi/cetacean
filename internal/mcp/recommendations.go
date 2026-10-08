package mcp

import (
	"context"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/recommendations"
)

// recommendationsResult is the envelope the recommendations widget renders. The
// same data is readable at cetacean://recommendations, but a host can only push
// a *tool* result into a widget — so this delegates to that resource read
// rather than reaching for the engine, keeping ACL filtering on one path.
type recommendationsResult struct {
	Items   []mcpRecommendation     `json:"items"`
	Total   int                     `json:"total"`
	Summary recommendations.Summary `json:"summary"`
}

// toolGetRecommendations returns the recommendations the caller may see. Total
// and Summary count the filtered set, not the engine's: a caller granted one
// stack is told how many findings they have, not how many the cluster holds.
func (s *Server) toolGetRecommendations(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	listed, err := s.lookupResource(ctx, "cetacean://recommendations")
	if err != nil {
		return "", err
	}

	// The resource read answers with an empty []any when the engine is off,
	// which is the honest answer — no recommendations, rather than an error.
	items, _ := listed.([]mcpRecommendation)

	severity := req.GetString("severity", "")
	if severity != "" {
		filtered := make([]mcpRecommendation, 0, len(items))

		for _, item := range items {
			if string(item.Severity) == severity {
				filtered = append(filtered, item)
			}
		}

		items = filtered
	}

	if items == nil {
		items = []mcpRecommendation{}
	}

	return marshalResult(recommendationsResult{
		Items:   items,
		Total:   len(items),
		Summary: recommendations.ComputeSummary(baseRecommendations(items)),
	})
}
