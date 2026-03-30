package recommendations

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
)

// ConfigChecker finds services with missing health checks or restart policies.
type ConfigChecker struct {
	cache *cache.Cache
}

// NewConfigChecker creates a new config hygiene checker.
func NewConfigChecker(c *cache.Cache) *ConfigChecker {
	return &ConfigChecker{cache: c}
}

func (cc *ConfigChecker) Name() string            { return "config" }
func (cc *ConfigChecker) Interval() time.Duration { return 60 * time.Second }

// Check inspects all services for missing health checks and restart policies.
func (cc *ConfigChecker) Check(_ context.Context) []Recommendation {
	var recs []Recommendation

	for _, svc := range cc.cache.ListServices() {
		cs := svc.Spec.TaskTemplate.ContainerSpec

		if cs == nil || cs.Healthcheck == nil ||
			(len(cs.Healthcheck.Test) >= 1 && cs.Healthcheck.Test[0] == "NONE") {
			recs = append(recs, Recommendation{
				Category:   CategoryNoHealthcheck,
				Severity:   SeverityWarning,
				Scope:      ScopeService,
				TargetID:   svc.ID,
				TargetName: svc.Spec.Name,
				Message:    "Service has no health check configured",
			})
		}

		rp := svc.Spec.TaskTemplate.RestartPolicy
		if rp != nil && rp.Condition == swarm.RestartPolicyConditionNone {
			recs = append(recs, Recommendation{
				Category:   CategoryNoRestartPolicy,
				Severity:   SeverityWarning,
				Scope:      ScopeService,
				TargetID:   svc.ID,
				TargetName: svc.Spec.Name,
				Message:    "Service has no restart policy configured",
			})
		}
	}

	return recs
}
