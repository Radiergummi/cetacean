package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// TestHistoryNamesTasksAfterTheirService pins the fix for what the live
// evaluation found in cetacean://history: a task's history entry carried its
// own ID as its name, so a reader looking at sixty task events could not tell
// which service any of them belonged to. "What broke overnight" was
// unanswerable from the one resource that exists to answer it.
//
// The ID stays in resourceId — it is what addresses the task — while the name
// becomes the one Swarm shows, so the entry reads the way `docker service ps`
// does. Compare a network event, which already carried a real name.
func TestHistoryNamesTasksAfterTheirService(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "demo_flaky"}},
	})
	c.SetTask(swarm.Task{
		ID:        "task1",
		ServiceID: "svc1",
		Slot:      3,
		Status:    swarm.TaskStatus{State: swarm.TaskStateFailed},
	})

	srv := newResourceTestServer(t, c)

	entries := readHistory(t, srv)

	task := findEntryByResourceID(t, entries, "task1")
	if got := task["name"]; got != "demo_flaky.3" {
		t.Errorf("task entry name = %v, want demo_flaky.3", got)
	}
	if got := task["resourceId"]; got != "task1" {
		t.Errorf("task entry resourceId = %v, want task1 (the ID must survive)", got)
	}
}

// TestHistoryLeavesNamedTypesAlone guards the other seven types: they already
// record a usable name, and the task fix must not reach them.
func TestHistoryLeavesNamedTypesAlone(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "demo_web"}},
	})

	srv := newResourceTestServer(t, c)

	entries := readHistory(t, srv)

	svc := findEntryByResourceID(t, entries, "svc1")
	if got := svc["name"]; got != "demo_web" {
		t.Errorf("service entry name = %v, want demo_web", got)
	}
}

// TestHistoryFallsBackToTheTaskIDWhenTheServiceIsGone covers the ordinary
// race: a task outlives the service record, or the event arrives before it.
// Naming is best-effort — an entry must still be returned.
func TestHistoryFallsBackToTheTaskIDWhenTheServiceIsGone(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{
		ID:        "orphan1",
		ServiceID: "missing",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateFailed},
	})

	srv := newResourceTestServer(t, c)

	entries := readHistory(t, srv)

	task := findEntryByResourceID(t, entries, "orphan1")
	if got := task["name"]; got != "orphan1" {
		t.Errorf("orphaned task entry name = %v, want the ID as fallback", got)
	}
}

func readHistory(t *testing.T, srv *Server) []map[string]any {
	t.Helper()

	body, err := srv.readResource(context.Background(), "cetacean://history")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	var entries []map[string]any
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	return entries
}

func findEntryByResourceID(t *testing.T, entries []map[string]any, id string) map[string]any {
	t.Helper()

	for _, e := range entries {
		if e["resourceId"] == id {
			return e
		}
	}

	t.Fatalf("no history entry for resourceId %q in %d entries", id, len(entries))

	return nil
}
