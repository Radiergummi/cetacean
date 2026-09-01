package mcp

import (
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// fakeRecommendationEngine stands in for the real engine, whose findings depend
// on Prometheus and on timers.
type fakeRecommendationEngine struct {
	results []recommendations.Recommendation
}

func (f *fakeRecommendationEngine) Results() []recommendations.Recommendation { return f.results }

func recommendationFixtures() []recommendations.Recommendation {
	return []recommendations.Recommendation{
		{
			Category:   recommendations.CategoryNoHealthcheck,
			Severity:   recommendations.SeverityWarning,
			Scope:      recommendations.ScopeService,
			TargetID:   "svc1",
			TargetName: "api",
			Message:    "no health check configured",
		},
		{
			Category:   recommendations.CategoryFlakyService,
			Severity:   recommendations.SeverityCritical,
			Scope:      recommendations.ScopeService,
			TargetID:   "svc2",
			TargetName: "secret-app",
			Message:    "restarting repeatedly",
		},
	}
}

func callGetRecommendations(t *testing.T, srv *Server, args map[string]any) recommendationsResult {
	t.Helper()

	td, ok := srv.findTool("get_recommendations")
	if !ok {
		t.Fatal("get_recommendations is not registered at OpsReadOnly")
	}

	text, err := td.handler(ctxWithIdentity(), newCallToolRequest("get_recommendations", args))
	if err != nil {
		t.Fatalf("get_recommendations: %v", err)
	}

	var result recommendationsResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("decode result: %v: %s", err, text)
	}

	return result
}

func recommendationsServer(t *testing.T, overrides ...func(*Options)) *Server {
	t.Helper()

	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "secret-app"}},
	})

	options := append([]func(*Options){
		func(o *Options) {
			o.Recommendations = &fakeRecommendationEngine{results: recommendationFixtures()}
		},
	}, overrides...)

	return newToolTestServer(t, c, &fakeWriteClient{}, config.OpsReadOnly, options...)
}

func TestGetRecommendationsSummarisesWhatItReturns(t *testing.T) {
	result := callGetRecommendations(t, recommendationsServer(t), map[string]any{})

	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}

	if result.Summary.Critical != 1 || result.Summary.Warning != 1 {
		t.Errorf("summary = %+v, want one critical and one warning", result.Summary)
	}
}

func TestGetRecommendationsFiltersBySeverity(t *testing.T) {
	result := callGetRecommendations(t, recommendationsServer(t), map[string]any{
		"severity": "critical",
	})

	if result.Total != 1 {
		t.Fatalf("total = %d, want 1", result.Total)
	}

	if result.Items[0].TargetName != "secret-app" {
		t.Errorf("kept %q, want the critical finding", result.Items[0].TargetName)
	}

	// The summary counts what the caller received, not what the engine holds.
	if result.Summary.Warning != 0 {
		t.Errorf("summary = %+v, want it to describe the filtered set", result.Summary)
	}
}

// A finding names a resource and says something about its state, so the ACL
// applies to it exactly as it does to the resource itself.
func TestGetRecommendationsAppliesACL(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:api"))

	result := callGetRecommendations(
		t,
		recommendationsServer(t, func(o *Options) { o.ACL = evaluator }),
		map[string]any{},
	)

	for _, item := range result.Items {
		if item.TargetName == "secret-app" {
			t.Errorf("finding about an unreadable service was returned: %+v", item)
		}
	}

	if result.Total != 1 {
		t.Errorf("total = %d, want only the readable finding", result.Total)
	}
}

// An engine that is switched off is not an error: the answer is that there is
// nothing to report, and it must still be a well-formed result.
func TestGetRecommendationsWithoutAnEngineReturnsAnEmptySet(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsReadOnly)

	result := callGetRecommendations(t, srv, map[string]any{})

	if result.Total != 0 || len(result.Items) != 0 {
		t.Errorf("result = %+v, want an empty set", result)
	}
}

// TestGetRecommendationsPointsAtItsWidget — see TestGetLogsPointsAtTheLogWidget.
func TestGetRecommendationsPointsAtItsWidget(t *testing.T) {
	srv := recommendationsServer(t)

	td, ok := srv.findTool("get_recommendations")
	if !ok {
		t.Fatal("get_recommendations is not registered at OpsReadOnly")
	}

	if td.widget != "recommendations" {
		t.Errorf("widget = %q, want recommendations", td.widget)
	}

	if td.tool.Meta == nil {
		t.Fatal("get_recommendations carries no _meta; hosts cannot find its widget")
	}

	want := uiResourceURI("recommendations")
	if got := td.tool.Meta.AdditionalFields[uiResourceURIMetaKey]; got != want {
		t.Errorf("_meta[%q] = %v, want %q", uiResourceURIMetaKey, got, want)
	}
}
