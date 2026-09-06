package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/docker"
)

// dockerNotFound is the error shape the daemon returns for a task whose record
// Swarm has already retired: the message verbatim from a live cluster, wrapped
// in the errdefs sentinel the Docker client attaches to every 404. The wrap is
// the part that matters — isNotFound classifies on the sentinel, so a fixture
// carrying only the text would pin the message rather than the rule.
//
//nolint:staticcheck // ST1005: quoted verbatim from the daemon, not authored here.
var dockerNotFound = fmt.Errorf(
	"Error response from daemon: task pgdsuwjhafl2b7699796mm8oo not found: %w",
	cerrdefs.ErrNotFound,
)

// TestTaskLogsExplainAPrunedRecord pins the fix for what the live evaluation
// hit: get_logs advertises a task read as "the only way to reach a replica
// that has already exited", and the most valuable moment to use it — right
// after a replica died — is exactly when Swarm has retired the record. The
// caller got the daemon's raw "task not found" and could not tell a pruned
// record from a mistyped ID, so the sensible next move (read the service, or
// describe it for its failures) was not discoverable from the failure.
func TestTaskLogsExplainAPrunedRecord(t *testing.T) {
	c := cache.New(nil)
	// The cache still holds the task, so this is a real ID whose output the
	// daemon will no longer serve — a pruned record, not a bad identifier.
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "demo_flaky"}},
	})
	c.SetTask(swarm.Task{
		ID:        "task1",
		ServiceID: "svc1",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateFailed},
	})

	srv := newLogTestServer(t, c, &fakeLogStreamer{err: dockerNotFound})

	_, err := srv.readLogsImpl(context.Background(), docker.TaskLog, "task1", logOptions{})
	if err == nil {
		t.Fatal("expected an error for a task whose logs the daemon will not serve")
	}

	msg := err.Error()

	// It must say the output is gone for good, so the caller stops retrying.
	if !strings.Contains(msg, "discard") && !strings.Contains(msg, "retriev") {
		t.Errorf("error does not explain that the output is gone: %q", msg)
	}

	// It must name the way forward, which is the parent service.
	if !strings.Contains(msg, "demo_flaky") {
		t.Errorf("error does not point at the parent service to read instead: %q", msg)
	}
}

// TestTaskLogsDistinguishAnUnknownID is the other half: an ID the cache has
// never seen is a different mistake with a different fix, and collapsing the
// two would trade one unactionable message for another.
func TestTaskLogsDistinguishAnUnknownID(t *testing.T) {
	c := cache.New(nil)

	srv := newLogTestServer(t, c, &fakeLogStreamer{err: dockerNotFound})

	_, err := srv.readLogsImpl(context.Background(), docker.TaskLog, "nosuchtask", logOptions{})
	if err == nil {
		t.Fatal("expected an error for an unknown task ID")
	}

	msg := err.Error()
	if !strings.Contains(msg, "no such task") && !strings.Contains(msg, "unknown task") {
		t.Errorf("error does not report an unknown identifier: %q", msg)
	}
}

// TestServiceLogsKeepTheirRawError guards the blast radius: the task-specific
// explanation must not swallow an ordinary daemon failure on the service path,
// where "not found" means what it says.
func TestServiceLogsKeepTheirRawError(t *testing.T) {
	c := cache.New(nil)

	srv := newLogTestServer(t, c, &fakeLogStreamer{err: errors.New("docker unavailable")})

	_, err := srv.readLogsImpl(context.Background(), docker.ServiceLog, "svc1", logOptions{})
	if err == nil || !strings.Contains(err.Error(), "docker unavailable") {
		t.Fatalf("expected the raw streamer error, got %v", err)
	}
}
