package imagecheck

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

func serviceRunning(image string) swarm.Service {
	return swarm.Service{Spec: swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{
		ContainerSpec: &swarm.ContainerSpec{Image: image},
	}}}
}

func newTestChecker(registry *fakeRegistry, services *[]swarm.Service) *Checker {
	c := New(func() []swarm.Service { return *services })
	c.client = registry.client()
	c.spacing = 0

	return c
}

func TestCheckerChecksEachReferenceOnce(t *testing.T) {
	registry := newFakeRegistry(t, func(r *fakeRegistry) { r.open = true })

	requests := &registry.manifestRequests

	host := strings.TrimPrefix(registry.URL, "https://")
	services := []swarm.Service{
		serviceRunning(host + "/library/app:1@" + digestA),
		serviceRunning(host + "/library/app:1@" + digestB),
		serviceRunning(host + "/library/app:1@" + digestA),
	}

	c := newTestChecker(registry, &services)
	c.scan(context.Background())

	if n := requests.Load(); n != 1 {
		t.Errorf("%d requests for one reference, want 1", n)
	}

	got, ok := c.LatestDigest(services[0].Spec.TaskTemplate.ContainerSpec.Image)
	if !ok || got.Digest != indexDigest {
		t.Fatalf("LatestDigest = %+v, %v", got, ok)
	}

	// Not due again until the interval has passed.
	c.scan(context.Background())

	if n := requests.Load(); n != 1 {
		t.Errorf("a second scan within the interval asked again (%d requests)", n)
	}

	now := time.Now().Add(checkInterval + time.Second)
	c.now = func() time.Time { return now }
	c.scan(context.Background())

	if n := requests.Load(); n != 2 {
		t.Errorf("%d requests after the interval, want 2", n)
	}
}

func TestCheckerForgetsUnusedReferences(t *testing.T) {
	registry := newFakeRegistry(t, func(r *fakeRegistry) { r.open = true })
	host := strings.TrimPrefix(registry.URL, "https://")
	image := host + "/library/app:1@" + digestA
	services := []swarm.Service{serviceRunning(image)}

	c := newTestChecker(registry, &services)
	c.scan(context.Background())

	before := c.Generation()
	services = nil
	c.scan(context.Background())

	if _, ok := c.LatestDigest(image); ok {
		t.Error("an image no service runs is still reported")
	}

	if c.Generation() == before {
		t.Error("forgetting an answer did not advance the generation")
	}
}

func TestCheckerSkipsPrivateImagesQuietly(t *testing.T) {
	registry := newFakeRegistry(t, func(r *fakeRegistry) { r.private = true })
	host := strings.TrimPrefix(registry.URL, "https://")
	image := host + "/library/app:1@" + digestA
	services := []swarm.Service{serviceRunning(image)}

	c := newTestChecker(registry, &services)
	c.scan(context.Background())

	if _, ok := c.LatestDigest(image); ok {
		t.Error("a private image has an answer")
	}

	e := c.entries[mustKey(t, image)]
	if e.failures != 0 || time.Until(e.nextAt) < checkInterval-time.Minute {
		t.Errorf("a private image is retried like a failure: %+v", e)
	}
}

func TestCheckerBacksOffAndKeepsTheLastAnswer(t *testing.T) {
	registry := newFakeRegistry(t, func(r *fakeRegistry) { r.open = true })
	host := strings.TrimPrefix(registry.URL, "https://")
	image := host + "/library/app:1@" + digestA
	services := []swarm.Service{serviceRunning(image)}

	c := newTestChecker(registry, &services)
	c.scan(context.Background())

	ref, _, _ := ParsePinned(image)
	for range 2 {
		c.record(ref, "", context.DeadlineExceeded)
	}

	got, ok := c.LatestDigest(image)
	if !ok || got.Digest != indexDigest {
		t.Errorf("a transient failure dropped the last answer: %+v, %v", got, ok)
	}

	if wait := time.Until(c.entries[ref.Key()].nextAt); wait > 2*firstRetry || wait < firstRetry {
		t.Errorf("second retry in %s, want about %s", wait, 2*firstRetry)
	}
}

func TestBackoffIsBoundedByTheInterval(t *testing.T) {
	if got := backoff(1); got != firstRetry {
		t.Errorf("backoff(1) = %s", got)
	}

	if got := backoff(50); got != checkInterval {
		t.Errorf("backoff(50) = %s, want %s", got, checkInterval)
	}
}

func TestNilCheckerKnowsNothing(t *testing.T) {
	var c *Checker

	if _, ok := c.LatestDigest("app:1@" + digestA); ok {
		t.Error("a nil checker answered")
	}

	if c.Generation() != 0 {
		t.Error("a nil checker has a generation")
	}
}

func mustKey(t *testing.T, image string) string {
	t.Helper()

	ref, _, ok := ParsePinned(image)
	if !ok {
		t.Fatalf("%q does not parse", image)
	}

	return ref.Key()
}
