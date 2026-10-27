package recommendations

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/versions"
)

// ConfigChecker finds services with missing health checks or restart policies,
// and old config and secret versions nothing uses any more.
type ConfigChecker struct {
	cache *cache.Cache
}

// NewConfigChecker creates a new config hygiene checker.
func NewConfigChecker(c *cache.Cache) *ConfigChecker {
	return &ConfigChecker{cache: c}
}

func (cc *ConfigChecker) Name() string            { return "config" }
func (cc *ConfigChecker) Interval() time.Duration { return 60 * time.Second }

// Check inspects all services for missing health checks and restart policies,
// and every config and secret for an unused old version.
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

	secrets := cc.cache.ListSecrets()
	secretVersions := versions.Secrets(secrets)
	for _, sec := range secrets {
		if m := secretVersions[sec.ID]; unusedVersion(m, cc.cache.ServicesUsingSecret(sec.ID)) {
			recs = append(recs, unusedVersionRec(ScopeSecret, sec.ID, sec.Spec.Name, m.Of))
		}
	}

	configs := cc.cache.ListConfigs()
	configVersions := versions.Configs(configs)
	for _, cfg := range configs {
		if m := configVersions[cfg.ID]; unusedVersion(m, cc.cache.ServicesUsingConfig(cfg.ID)) {
			recs = append(recs, unusedVersionRec(ScopeConfig, cfg.ID, cfg.Spec.Name, m.Of))
		}
	}

	return recs
}

// unusedVersion reports an older version of a grouped config or secret that no
// service references. The newest is never one: it is what a rotation deploys next.
func unusedVersion(m versions.Membership, users []cache.ServiceRef) bool {
	return m.Of != "" && !m.Newest && len(users) == 0
}

func unusedVersionRec(scope Scope, id, name, of string) Recommendation {
	return Recommendation{
		Category:   CategoryUnusedVersion,
		Severity:   SeverityInfo,
		Scope:      scope,
		TargetID:   id,
		TargetName: name,
		Message:    fmt.Sprintf("An older version of %s %q that no service uses", scope, of),
	}
}
