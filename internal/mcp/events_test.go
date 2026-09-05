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

// The filters are applied after the ring returns, so the read has to see the
// whole ring. Reading a page of it means task churn — which is most of what a
// busy cluster records — fills the window, and "what changed about the
// services?" comes back empty with truncated: false, saying "that is all there
// was" about a window the caller never asked for.
func TestGetEventsFindsAnEventBuriedUnderTaskChurn(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	for range maxEventLimit * 5 {
		c.History().Append(cache.HistoryEntry{
			Type:       cache.EventTask,
			Action:     "update",
			ResourceID: "t1",
			Name:       "web.1",
		})
	}

	got := eventsCall(t, newResourceTestServer(t, c), map[string]any{
		"types": []any{"service"},
	})

	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want the one service event under the churn", len(got.Entries))
	}
	if got.Total != 1 {
		t.Errorf("total = %d, want 1", got.Total)
	}
	if got.Truncated {
		t.Error("truncated = true, but the whole answer was returned")
	}
}

// total counts what matched, not what the ring held, and truncated says a page
// was cut — a caller narrowing by time reads both to know whether to narrow
// further.
func TestGetEventsCountsEveryMatchNotJustThePage(t *testing.T) {
	c := cache.New(nil)
	for range 250 {
		c.History().Append(cache.HistoryEntry{
			Type:       cache.EventService,
			Action:     "update",
			ResourceID: "svc1",
			Name:       "web",
		})
	}

	got := eventsCall(t, newResourceTestServer(t, c), map[string]any{
		"limit": float64(10),
	})

	if len(got.Entries) != 10 {
		t.Fatalf("entries = %d, want the requested page of 10", len(got.Entries))
	}
	if got.Total != 250 {
		t.Errorf("total = %d, want every match counted", got.Total)
	}
	if !got.Truncated {
		t.Error("truncated = false, but 240 matches were cut")
	}
}

// A zero limit is how a caller spells "no preference". Reading it as the
// ceiling returns five times the documented default.
func TestGetEventsTreatsAZeroLimitAsTheDefault(t *testing.T) {
	c := cache.New(nil)
	for range defaultEventLimit * 2 {
		c.History().Append(cache.HistoryEntry{
			Type:       cache.EventService,
			Action:     "update",
			ResourceID: "svc1",
			Name:       "web",
		})
	}

	got := eventsCall(t, newResourceTestServer(t, c), map[string]any{"limit": float64(0)})

	if len(got.Entries) != defaultEventLimit {
		t.Errorf("entries = %d, want the default of %d", len(got.Entries), defaultEventLimit)
	}
}
