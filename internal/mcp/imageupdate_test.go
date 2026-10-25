package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/imagecheck"
)

type fixedImageDigests map[string]imagecheck.Result

func (f fixedImageDigests) LatestDigest(image string) (imagecheck.Result, bool) {
	r, ok := f[image]
	return r, ok
}

// The rule is pinned in internal/cluster; this pins that describe reaches it.
func TestDescribeServiceReportsANewerImage(t *testing.T) {
	const (
		running = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		moved   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		image   = "nginx:1.27@" + running
	)

	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: image}},
		},
	})

	srv := newResourceTestServer(t, c)

	digest, err := srv.digestOf(context.Background(), "service", mustService(t, c, "svc1"))
	if err != nil {
		t.Fatalf("digestOf: %v", err)
	}

	if _, ok := digest.Details["imageUpdate"]; ok {
		t.Error("imageUpdate reported with no image check configured")
	}

	srv.images = fixedImageDigests{image: {Digest: moved, CheckedAt: time.Now()}}

	digest, err = srv.digestOf(context.Background(), "service", mustService(t, c, "svc1"))
	if err != nil {
		t.Fatalf("digestOf: %v", err)
	}

	update, ok := digest.Details["imageUpdate"].(*cluster.ImageUpdate)
	if !ok || update.SuggestedImage != "nginx:1.27@"+moved {
		t.Errorf("details.imageUpdate = %#v", digest.Details["imageUpdate"])
	}
}
