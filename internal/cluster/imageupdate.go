package cluster

import (
	"strings"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/imagecheck"
)

// ServiceImage is the reference an image update names, without the whitespace
// Docker would otherwise keep in it; empty means none was named.
func ServiceImage(raw string) string { return strings.TrimSpace(raw) }

// ImageDigests answers what a registry last said an image's tag names.
type ImageDigests interface {
	LatestDigest(image string) (imagecheck.Result, bool)
}

// ImageUpdate reports that a service's image tag names a newer image than the
// digest the service runs.
type ImageUpdate struct {
	// Image is the tag the service follows, as its spec spells it.
	Image          string `json:"image"`
	CurrentDigest  string `json:"currentDigest"`
	LatestDigest   string `json:"latestDigest"`
	SuggestedImage string `json:"suggestedImage"`
	CheckedAt      string `json:"checkedAt"`

	// Since is when the registry was first seen to name LatestDigest, which a
	// response carrying this dates itself by.
	Since time.Time `json:"-"`
}

// ServiceImageUpdate compares the digest svc pins with the one digests last
// saw its tag name, and is nil unless they differ. The suggestion pins the
// new digest, so updating to it deploys exactly the image that was checked.
func ServiceImageUpdate(svc swarm.Service, digests ImageDigests) *ImageUpdate {
	spec := svc.Spec.TaskTemplate.ContainerSpec
	if digests == nil || spec == nil {
		return nil
	}

	name, current, pinned := strings.Cut(spec.Image, "@")
	if !pinned {
		return nil
	}

	latest, ok := digests.LatestDigest(spec.Image)
	if !ok || latest.Digest == current {
		return nil
	}

	return &ImageUpdate{
		Image:          name,
		CurrentDigest:  current,
		LatestDigest:   latest.Digest,
		SuggestedImage: name + "@" + latest.Digest,
		CheckedAt:      latest.CheckedAt.UTC().Format(time.RFC3339),
		Since:          latest.Since,
	}
}
