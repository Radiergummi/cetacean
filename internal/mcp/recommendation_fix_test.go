package mcp

import (
	"encoding/json"
	"testing"

	"github.com/radiergummi/cetacean/internal/recommendations"
)

// TestRecommendationFixNamesATool pins the fix for what the live evaluation
// found: get_recommendations handed back `"fixAction": "PUT /nodes/{id}/availability"`.
//
// That is the REST transport's vocabulary. An MCP caller has tools, not
// routes, and cannot issue an HTTP request at all — so the one field on the
// finding that is supposed to say what to do next says something the reader
// is structurally unable to do. The REST field stays exactly as it is, because
// the dashboard string-matches on it (frontend/src/lib/applyRecommendation.ts);
// the translation belongs at the MCP boundary.
func TestRecommendationFixNamesATool(t *testing.T) {
	cases := []struct {
		name        string
		fixAction   string
		wantTool    string
		wantSection string
	}{
		{
			name:      "scale is its own tool",
			fixAction: "PUT /services/{id}/scale",
			wantTool:  "scale_service",
		},
		{
			name:        "availability is a section of update_node",
			fixAction:   "PUT /nodes/{id}/availability",
			wantTool:    "update_node",
			wantSection: "availability",
		},
		{
			name:        "resources is a section of update_service",
			fixAction:   "PATCH /services/{id}/resources",
			wantTool:    "update_service",
			wantSection: "resources",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := projectRecommendations([]recommendations.Recommendation{{
				Category:  "test",
				Severity:  recommendations.SeverityWarning,
				TargetID:  "abc",
				FixAction: &tc.fixAction,
			}})

			if len(got) != 1 {
				t.Fatalf("projected %d recommendations, want 1", len(got))
			}

			fix := got[0].Fix
			if fix == nil {
				t.Fatal("Fix is nil; the finding does not say what to call")
			}
			if fix.Tool != tc.wantTool {
				t.Errorf("Tool = %q, want %q", fix.Tool, tc.wantTool)
			}
			if fix.Section != tc.wantSection {
				t.Errorf("Section = %q, want %q", fix.Section, tc.wantSection)
			}
		})
	}
}

// TestRecommendationDropsTheRESTPath is the other half: leaving the route
// beside the tool would let a model pick the one it cannot use.
func TestRecommendationDropsTheRESTPath(t *testing.T) {
	action := "PUT /services/{id}/scale"

	got := projectRecommendations([]recommendations.Recommendation{{
		Category:  "single-replica",
		TargetID:  "svc1",
		FixAction: &action,
	}})

	encoded, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := fields["fixAction"]; ok {
		t.Errorf("fixAction still on the wire: %s", encoded)
	}
	if _, ok := fields["fix"]; !ok {
		t.Errorf("fix missing from the wire: %s", encoded)
	}

	// The rest of the finding must survive the projection untouched.
	if fields["category"] != "single-replica" {
		t.Errorf("category = %v, want single-replica", fields["category"])
	}
	if fields["targetId"] != "svc1" {
		t.Errorf("targetId = %v, want svc1", fields["targetId"])
	}
}

// TestRecommendationWithoutAFixStaysBare keeps findings that carry no remedy —
// no-healthcheck, flaky-service — free of an invented one.
func TestRecommendationWithoutAFixStaysBare(t *testing.T) {
	got := projectRecommendations([]recommendations.Recommendation{{
		Category: "no-healthcheck",
		TargetID: "svc1",
	}})

	if got[0].Fix != nil {
		t.Errorf("Fix = %+v, want nil for a finding with no remedy", got[0].Fix)
	}
}

// TestRecommendationWithAnUnmappedFixDropsIt guards the failure mode that
// matters if a new REST fixAction is added and this table is not: emitting the
// route anyway would reintroduce the defect silently.
func TestRecommendationWithAnUnmappedFixDropsIt(t *testing.T) {
	action := "POST /services/{id}/something-new"

	got := projectRecommendations([]recommendations.Recommendation{{
		Category:  "future",
		TargetID:  "svc1",
		FixAction: &action,
	}})

	if got[0].Fix != nil {
		t.Errorf("Fix = %+v, want nil for a route with no tool behind it", got[0].Fix)
	}
}
