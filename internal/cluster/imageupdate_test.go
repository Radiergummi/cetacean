package cluster

import (
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/imagecheck"
)

const (
	runningDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newerDigest   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type fixedDigests map[string]imagecheck.Result

func (f fixedDigests) LatestDigest(image string) (imagecheck.Result, bool) {
	r, ok := f[image]
	return r, ok
}

func serviceWithImage(image string) swarm.Service {
	return swarm.Service{Spec: swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{
		ContainerSpec: &swarm.ContainerSpec{Image: image},
	}}}
}

func TestServiceImageUpdate(t *testing.T) {
	image := "nginx:1.27@" + runningDigest
	checked := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	t.Run("a moved tag suggests the new digest", func(t *testing.T) {
		got := ServiceImageUpdate(serviceWithImage(image), fixedDigests{
			image: {Digest: newerDigest, CheckedAt: checked, Since: checked},
		})

		if got == nil {
			t.Fatal("no update reported")
		}

		want := ImageUpdate{
			Image:          "nginx:1.27",
			CurrentDigest:  runningDigest,
			LatestDigest:   newerDigest,
			SuggestedImage: "nginx:1.27@" + newerDigest,
			CheckedAt:      "2026-10-01T12:00:00Z",
			Since:          checked,
		}
		if *got != want {
			t.Errorf("got %+v\nwant %+v", *got, want)
		}
	})

	cases := map[string]struct {
		svc     swarm.Service
		digests ImageDigests
	}{
		"the tag still names the running digest": {
			serviceWithImage(image),
			fixedDigests{image: {Digest: runningDigest}},
		},
		"nothing known about the tag": {serviceWithImage(image), fixedDigests{}},
		"no check configured":         {serviceWithImage(image), nil},
		"no pinned digest": {
			serviceWithImage("nginx:1.27"),
			fixedDigests{"nginx:1.27": {Digest: newerDigest}},
		},
		"no container spec": {swarm.Service{}, fixedDigests{}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := ServiceImageUpdate(tc.svc, tc.digests); got != nil {
				t.Errorf("got %+v, want none", *got)
			}
		})
	}
}
