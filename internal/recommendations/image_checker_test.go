package recommendations

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestImageChecker(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "moved", Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "web"},
	}})
	c.SetService(swarm.Service{ID: "current", Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "api"},
	}})

	checker := NewImageChecker(c, func(svc swarm.Service) (string, bool) {
		return "nginx:1.27", svc.ID == "moved"
	})

	recs := checker.Check(context.Background())
	if len(recs) != 1 {
		t.Fatalf("got %d recommendations, want 1: %+v", len(recs), recs)
	}

	got := recs[0]
	if got.Category != CategoryNewerImage || got.TargetID != "moved" ||
		got.Scope != ScopeService || got.Severity != SeverityInfo {
		t.Errorf("got %+v", got)
	}

	if got.Message != "A newer image exists for nginx:1.27" {
		t.Errorf("message = %q", got.Message)
	}

	if got.FixAction != nil {
		t.Errorf(
			"FixAction = %q; the suggestion is an image, not a number to apply",
			*got.FixAction,
		)
	}
}
