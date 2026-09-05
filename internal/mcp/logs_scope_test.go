package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/docker"
)

// failOnceStreamer fails the first read and serves frames thereafter, so a
// fan-out test can assert partial success rather than all-or-nothing.
type failOnceStreamer struct {
	frames []byte
	failed bool
	mu     sync.Mutex
}

func (f *failOnceStreamer) Logs(
	_ context.Context,
	_ docker.LogKind,
	_, _ string,
	_ bool,
	_, _ string,
) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.failed {
		f.failed = true

		return nil, errors.New("docker unavailable")
	}

	return io.NopCloser(bytes.NewReader(f.frames)), nil
}

// The saving this task exists for: one call instead of one per service, with
// the merge done server-side.
func TestClusterScopeReadsEveryService(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"web", "api"} {
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	// One line per service, merged into one response.
	if len(got.Lines) != 2 {
		t.Fatalf("lines = %d, want 2 (one per service)", len(got.Lines))
	}
}

// Every line must say which service it came from, or a merged read is
// unreadable.
func TestClusterScopeAttributesEachLine(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if got.Lines[0].Attrs["serviceName"] != "web" {
		t.Errorf("serviceName = %v, want web", got.Lines[0].Attrs["serviceName"])
	}
}

// grep-the-cluster, done server-side: the caller pays for matches, not for
// every line of every service.
func TestContainsFiltersServerSide(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	streamer := &fakeLogStreamer{
		frames: append(
			buildLogFrame(1, "2026-09-05T14:00:00.000000000Z connection refused\n"),
			buildLogFrame(1, "2026-09-05T14:00:01.000000000Z all good\n")...,
		),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(
		context.Background(), "cluster", "",
		logOptions{tail: 10, contains: "refused"},
	)
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("lines = %d, want 1 match", len(got.Lines))
	}
	if !strings.Contains(got.Lines[0].Message, "refused") {
		t.Errorf("kept the wrong line: %q", got.Lines[0].Message)
	}
}

// A stack scope reads only its own members.
func TestStackScopeReadsOnlyItsMembers(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
			Name:   "demo_web",
			Labels: map[string]string{"com.docker.stack.namespace": "demo"},
		}},
	})
	c.SetService(swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
			Name:   "other_api",
			Labels: map[string]string{"com.docker.stack.namespace": "other"},
		}},
	})

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "stack", "demo", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("lines = %d, want 1 (only the demo stack's service)", len(got.Lines))
	}
	if got.Lines[0].Attrs["serviceName"] != "demo_web" {
		t.Errorf("read the wrong service: %v", got.Lines[0].Attrs["serviceName"])
	}
}

// One unreadable service must not fail the whole read: a cluster-wide grep
// that dies on the first broken service is worse than no tool.
func TestScopedReadSurvivesOneFailingService(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"web", "api"} {
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	srv := newLogTestServer(t, c, &failOnceStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	})

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Errorf("lines = %d, want the one service that worked", len(got.Lines))
	}
	if len(got.Errors) != 1 {
		t.Errorf("errors = %d, want the one that failed reported", len(got.Errors))
	}
}
