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

	secrets := versions.SecretItems(cc.cache.ListSecrets())
	configs := versions.ConfigItems(cc.cache.ListConfigs())

	recs = append(recs, unusedVersions(ScopeSecret, secrets, cc.cache.ServicesUsingSecret)...)
	recs = append(recs, unusedVersions(ScopeConfig, configs, cc.cache.ServicesUsingConfig)...)

	return recs
}

// unusedVersions flags each older version of a grouped config or secret that
// no service references. The newest is never one: it is what a rotation
// deploys next. A finding discloses that the newest exists, so it Reveals it.
func unusedVersions(
	scope Scope,
	items []versions.Item,
	users func(id string) []cache.ServiceRef,
) []Recommendation {
	grouped := versions.Group(items)

	newest := map[versions.GroupKey]string{}
	for _, it := range items {
		if m, ok := grouped[it.ID]; ok && m.Rank == 0 {
			newest[versions.GroupKey{Stack: m.Stack, Name: m.Of}] = it.Name
		}
	}

	var recs []Recommendation
	for _, it := range items {
		m, ok := grouped[it.ID]
		if !ok || m.Rank == 0 || len(users(it.ID)) > 0 {
			continue
		}

		head := newest[versions.GroupKey{Stack: m.Stack, Name: m.Of}]
		recs = append(recs, Recommendation{
			Category:   CategoryUnusedVersion,
			Severity:   SeverityInfo,
			Scope:      scope,
			TargetID:   it.ID,
			TargetName: it.Name,
			Message:    fmt.Sprintf("An older version of %s %q that no service uses", scope, m.Of),
			Reveals:    []string{string(scope) + ":" + head},
		})
	}

	return recs
}
