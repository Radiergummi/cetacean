package recommendations

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// NewerImageFunc names the tag whose newer image a service could run, and
// reports false when its tag still names what it runs.
type NewerImageFunc func(svc swarm.Service) (tag string, ok bool)

// ImageChecker finds services whose image tag has moved past what they run.
type ImageChecker struct {
	cache *cache.Cache
	newer NewerImageFunc
}

// NewImageChecker creates a checker reporting what newer reports.
func NewImageChecker(c *cache.Cache, newer NewerImageFunc) *ImageChecker {
	return &ImageChecker{cache: c, newer: newer}
}

func (ic *ImageChecker) Name() string            { return "image" }
func (ic *ImageChecker) Interval() time.Duration { return 60 * time.Second }

// Check reports each service a newer image exists for.
func (ic *ImageChecker) Check(_ context.Context) []Recommendation {
	var recs []Recommendation

	for _, svc := range ic.cache.ListServices() {
		tag, ok := ic.newer(svc)
		if !ok {
			continue
		}

		recs = append(recs, Recommendation{
			Category:   CategoryNewerImage,
			Severity:   SeverityInfo,
			Scope:      ScopeService,
			TargetID:   svc.ID,
			TargetName: svc.Spec.Name,
			Message:    "A newer image exists for " + tag,
		})
	}

	return recs
}
