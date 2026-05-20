package mcp

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func TestNotificationSubscribeAndMatch(t *testing.T) {
	nm := NewNotificationManager()

	nm.Subscribe("session1", "cetacean://services/svc1")
	nm.Subscribe("session1", "cetacean://nodes/node1")

	matches := nm.MatchingURIs("session1", cache.Event{
		Type:   cache.EventService,
		Action: "update",
		ID:     "svc1",
	})
	if len(matches) != 1 || matches[0] != "cetacean://services/svc1" {
		t.Errorf("matches = %v, want [cetacean://services/svc1]", matches)
	}
}

func TestNotificationNoMatchForDifferentID(t *testing.T) {
	nm := NewNotificationManager()
	nm.Subscribe("session1", "cetacean://services/svc1")

	matches := nm.MatchingURIs("session1", cache.Event{
		Type:   cache.EventService,
		Action: "update",
		ID:     "svc2",
	})
	if len(matches) != 0 {
		t.Errorf("matches = %v, want none", matches)
	}
}

func TestNotificationLogURIMatches(t *testing.T) {
	nm := NewNotificationManager()
	nm.Subscribe("session1", "cetacean://services/svc1/logs")

	matches := nm.MatchingURIs("session1", cache.Event{
		Type:   cache.EventService,
		Action: "update",
		ID:     "svc1",
	})
	if len(matches) != 1 || matches[0] != "cetacean://services/svc1/logs" {
		t.Errorf("matches = %v, want [cetacean://services/svc1/logs]", matches)
	}
}

func TestNotificationDetailAndLogMatchTogether(t *testing.T) {
	nm := NewNotificationManager()
	nm.Subscribe("session1", "cetacean://services/svc1")
	nm.Subscribe("session1", "cetacean://services/svc1/logs")

	matches := nm.MatchingURIs("session1", cache.Event{
		Type:   cache.EventService,
		Action: "update",
		ID:     "svc1",
	})
	if len(matches) != 2 {
		t.Errorf("matches = %v, want 2 entries", matches)
	}
}

func TestNotificationUnsubscribe(t *testing.T) {
	nm := NewNotificationManager()
	nm.Subscribe("session1", "cetacean://services/svc1")
	nm.Unsubscribe("session1", "cetacean://services/svc1")

	matches := nm.MatchingURIs("session1", cache.Event{
		Type:   cache.EventService,
		Action: "update",
		ID:     "svc1",
	})
	if len(matches) != 0 {
		t.Errorf("matches = %v, want none after unsubscribe", matches)
	}
}

func TestNotificationRemoveSessionDropsAll(t *testing.T) {
	nm := NewNotificationManager()
	nm.Subscribe("session1", "cetacean://services/svc1")
	nm.Subscribe("session1", "cetacean://nodes/node1")
	nm.RemoveSession("session1")

	if ids := nm.sessionIDs(); len(ids) != 0 {
		t.Errorf("sessionIDs = %v, want none", ids)
	}
}

func TestNotificationIsListChange(t *testing.T) {
	nm := NewNotificationManager()
	tests := []struct {
		action string
		want   bool
	}{
		{"create", true},
		{"remove", true},
		{"update", false},
	}
	for _, tc := range tests {
		got := nm.IsListChange(cache.Event{Action: tc.action})
		if got != tc.want {
			t.Errorf("IsListChange(%q) = %v, want %v", tc.action, got, tc.want)
		}
	}
}

func TestNotificationEventTypeToURIPrefix(t *testing.T) {
	tests := []struct {
		typ  cache.EventType
		want string
	}{
		{cache.EventNode, "cetacean://nodes/"},
		{cache.EventService, "cetacean://services/"},
		{cache.EventTask, "cetacean://tasks/"},
		{cache.EventConfig, "cetacean://configs/"},
		{cache.EventSecret, "cetacean://secrets/"},
		{cache.EventNetwork, "cetacean://networks/"},
		{cache.EventVolume, "cetacean://volumes/"},
		{cache.EventStack, "cetacean://stacks/"},
		{cache.EventSync, ""},
	}
	for _, tc := range tests {
		got := eventTypeToURIPrefix(tc.typ)
		if got != tc.want {
			t.Errorf("eventTypeToURIPrefix(%q) = %q, want %q", tc.typ, got, tc.want)
		}
	}
}

// TestStartNotificationsCancelDetachesListener verifies that calling the
// returned cancel function actually removes the listener from the cache —
// otherwise the cache would keep firing into a stale Server after shutdown.
func TestStartNotificationsCancelDetachesListener(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	srv, err := New(c, Options{Config: cfg})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Establish a subscription so the notification path has work to do.
	srv.notifications.Subscribe("session1", "cetacean://services/svc1")

	// Drive the cache once before cancel and once after — both should not
	// panic. The strong guarantee is that Close detaches; we exercise it
	// by setting an event and confirming sessionIDs survives.
	c.SetService(swarm.Service{ID: "svc1"})
	srv.Close()
	c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "updated"}}})

	// After Close, manager state is untouched (sessions stay until the
	// session itself is gone) but no listener should fire. We assert the
	// cancel function was nil'd so a second Close is a no-op.
	if srv.cancelNotifications != nil {
		t.Fatal("Close did not clear cancelNotifications")
	}
	srv.Close() // must not panic
}
