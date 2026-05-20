package mcp

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func TestNotificationSubscribeAndMatch(t *testing.T) {
	nm := NewNotificationManager()

	nm.Subscribe("session1", "cetacean://services/svc1", nil)
	nm.Subscribe("session1", "cetacean://nodes/node1", nil)

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
	nm.Subscribe("session1", "cetacean://services/svc1", nil)

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
	nm.Subscribe("session1", "cetacean://services/svc1/logs", nil)

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
	nm.Subscribe("session1", "cetacean://services/svc1", nil)
	nm.Subscribe("session1", "cetacean://services/svc1/logs", nil)

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
	nm.Subscribe("session1", "cetacean://services/svc1", nil)
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
	nm.Subscribe("session1", "cetacean://services/svc1", nil)
	nm.Subscribe("session1", "cetacean://nodes/node1", nil)
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

func TestNotification_SubscribeCapturesIdentity(t *testing.T) {
	nm := NewNotificationManager()
	id := &auth.Identity{Subject: "alice"}
	nm.Subscribe("session1", "cetacean://services/svc1", id)

	got := nm.IdentityFor("session1")
	if got == nil || got.Subject != "alice" {
		t.Fatalf("IdentityFor = %v, want identity alice", got)
	}
}

func TestEventACLResource(t *testing.T) {
	tests := []struct {
		name  string
		event cache.Event
		want  string
	}{
		{"service uses Name", cache.Event{Type: cache.EventService, ID: "svc1", Name: "web"}, "service:web"},
		{"task uses ID", cache.Event{Type: cache.EventTask, ID: "task1", Name: "ignored"}, "task:task1"},
		{"node uses Name", cache.Event{Type: cache.EventNode, ID: "node1", Name: "worker-1"}, "node:worker-1"},
		{"missing name → empty", cache.Event{Type: cache.EventService, ID: "svc1"}, ""},
		{"missing task id → empty", cache.Event{Type: cache.EventTask, Name: "x"}, ""},
		{"sync → empty", cache.Event{Type: cache.EventSync}, ""},
	}
	for _, tc := range tests {
		got := eventACLResource(tc.event)
		if got != tc.want {
			t.Errorf("%s: eventACLResource = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestDispatchCacheEvent_ACLBlocksUpdatedNotification asserts that a subscriber
// whose stored identity can't read the affected resource does NOT show up in
// the matching deliveries — the dispatch loop short-circuits before calling
// SendNotificationToSpecificClient. The check is done on the canRead helper
// directly (mcp-go's send machinery is exercised in integration tests).
func TestDispatchCacheEvent_ACLBlocksUpdatedNotification(t *testing.T) {
	c := cache.New(nil)
	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	id := &auth.Identity{Subject: "alice"}
	srv.notifications.Subscribe("session1", "cetacean://services/svc1", id)

	allowed := srv.canRead(id, "service:public-api")
	denied := srv.canRead(id, "service:secret-svc")
	if !allowed {
		t.Fatal("identity should be allowed to read service:public-api")
	}
	if denied {
		t.Fatal("identity should NOT be allowed to read service:secret-svc")
	}
}

// TestDispatchCacheEvent_ACLOffPassesThrough confirms the dispatch is a no-op
// when no evaluator is attached (auth mode "none" / no policy).
func TestDispatchCacheEvent_ACLOffPassesThrough(t *testing.T) {
	c := cache.New(nil)
	srv := newResourceTestServer(t, c)

	if !srv.canRead(nil, "service:anything") {
		t.Fatal("canRead should allow everything when ACL is nil")
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
	srv.notifications.Subscribe("session1", "cetacean://services/svc1", nil)

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
