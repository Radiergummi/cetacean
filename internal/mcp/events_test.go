package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func eventsCall(t *testing.T, srv *Server, args map[string]any) eventsResult {
	t.Helper()

	body, err := srv.toolGetEvents(context.Background(), toolRequest(t, "get_events", args))
	if err != nil {
		t.Fatalf("toolGetEvents: %v", err)
	}

	var got eventsResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	return got
}

// The filter that makes the difference: a restarting service buries every
// other change under task churn, and "what changed about the services?" is
// the question that churn hides.
func TestGetEventsFiltersByType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	for _, id := range []string{"t1", "t2", "t3"} {
		c.SetTask(swarm.Task{ID: id, ServiceID: "svc1", Slot: 1})
	}

	srv := newResourceTestServer(t, c)

	got := eventsCall(t, srv, map[string]any{"types": []any{"service"}})

	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want 1 service event", len(got.Entries))
	}
	if got.Entries[0].Type != "service" {
		t.Errorf("type = %q, want service", got.Entries[0].Type)
	}
}

// Task entries carry the name their service gives them, for the same reason
// cetacean://history does: an ID names nothing.
func TestGetEventsNamesTasksAfterTheirService(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "demo_flaky"}},
	})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Slot: 3})

	srv := newResourceTestServer(t, c)

	got := eventsCall(t, srv, map[string]any{"types": []any{"task"}})

	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.Entries))
	}
	if got.Entries[0].Name != "demo_flaky.3" {
		t.Errorf("name = %q, want demo_flaky.3", got.Entries[0].Name)
	}
}

// "What broke overnight" is a time question, and answering it was structurally
// impossible while the window was a fixed count of newest entries.
func TestGetEventsFiltersBySince(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	srv := newResourceTestServer(t, c)

	// Far in the future: nothing recorded can be newer.
	got := eventsCall(t, srv, map[string]any{"since": "2099-01-01T00:00:00Z"})

	if len(got.Entries) != 0 {
		t.Errorf("entries = %d, want 0 for a since in the future", len(got.Entries))
	}
}

func TestGetEventsRejectsAnUnparseableSince(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	_, err := srv.toolGetEvents(
		context.Background(),
		toolRequest(t, "get_events", map[string]any{"since": "yesterday"}),
	)
	if err == nil {
		t.Fatal("expected an error naming the expected time format")
	}
}
