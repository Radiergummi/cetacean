package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"cetacean/internal/cache"
)

func TestNotifier_FiresWebhook(t *testing.T) {
	var calls atomic.Int32
	var received WebhookPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rules := []Rule{
		{ID: "r1", Name: "task failures", Enabled: true, Match: Match{Type: "task", Action: "update"}, Webhook: srv.URL},
	}
	for i := range rules {
		if err := rules[i].compile(); err != nil {
			t.Fatal(err)
		}
	}

	n := New(rules)
	n.HandleEvent(cache.Event{Type: "task", Action: "update", ID: "task-123"}, "web.1")

	time.Sleep(100 * time.Millisecond)

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 webhook call, got %d", got)
	}
	if received.Rule != "task failures" {
		t.Errorf("expected rule name %q, got %q", "task failures", received.Rule)
	}
	if received.Event.Type != "task" {
		t.Errorf("expected event type %q, got %q", "task", received.Event.Type)
	}
	if received.Event.Action != "update" {
		t.Errorf("expected event action %q, got %q", "update", received.Event.Action)
	}
	if received.Event.ResourceID != "task-123" {
		t.Errorf("expected resource ID %q, got %q", "task-123", received.Event.ResourceID)
	}
	if received.Event.Name != "web.1" {
		t.Errorf("expected name %q, got %q", "web.1", received.Event.Name)
	}
	if received.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestNotifier_RespectsCooldown(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rules := []Rule{
		{ID: "r1", Name: "test", Enabled: true, Match: Match{Type: "task"}, Webhook: srv.URL, Cooldown: "10s"},
	}
	for i := range rules {
		if err := rules[i].compile(); err != nil {
			t.Fatal(err)
		}
	}

	n := New(rules)

	for i := 0; i < 3; i++ {
		n.HandleEvent(cache.Event{Type: "task", Action: "update", ID: "t1"}, "web.1")
	}

	time.Sleep(100 * time.Millisecond)

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 webhook call (cooldown), got %d", got)
	}
}

func TestNotifier_NoMatchNoFire(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rules := []Rule{
		{ID: "r1", Name: "test", Enabled: true, Match: Match{Type: "node"}, Webhook: srv.URL},
	}
	for i := range rules {
		if err := rules[i].compile(); err != nil {
			t.Fatal(err)
		}
	}

	n := New(rules)
	n.HandleEvent(cache.Event{Type: "task", Action: "update", ID: "t1"}, "web.1")

	time.Sleep(100 * time.Millisecond)

	if got := calls.Load(); got != 0 {
		t.Fatalf("expected 0 webhook calls, got %d", got)
	}
}

func TestNotifier_CircuitBreaker(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError) // always fail
	}))
	defer srv.Close()

	rules := []Rule{
		{ID: "r1", Name: "test", Enabled: true, Match: Match{Type: "task"}, Webhook: srv.URL},
	}
	for i := range rules {
		if err := rules[i].compile(); err != nil {
			t.Fatal(err)
		}
	}

	n := New(rules)
	event := cache.Event{Type: "task", Action: "update", ID: "t1"}

	// Fire 5 events to trip the circuit breaker (threshold=5)
	for i := 0; i < 6; i++ {
		n.HandleEvent(event, "web.1")
		time.Sleep(50 * time.Millisecond)
	}

	callsBefore := calls.Load()

	// Now the circuit should be open — further events should be blocked
	n.HandleEvent(event, "web.1")
	time.Sleep(50 * time.Millisecond)

	if got := calls.Load(); got != callsBefore {
		t.Errorf("expected no new calls after circuit opens, got %d (was %d)", got, callsBefore)
	}

	// Verify status reports circuit open
	statuses := n.RuleStatuses()
	if !statuses[0].CircuitOpen {
		t.Error("expected CircuitOpen=true")
	}
	if statuses[0].ConsecFailures < 5 {
		t.Errorf("expected ConsecFailures>=5, got %d", statuses[0].ConsecFailures)
	}
}

func TestNotifier_CircuitResetsOnSuccess(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	rules := []Rule{
		{ID: "r1", Name: "test", Enabled: true, Match: Match{Type: "task"}, Webhook: srv.URL},
	}
	for i := range rules {
		if err := rules[i].compile(); err != nil {
			t.Fatal(err)
		}
	}

	n := New(rules)
	event := cache.Event{Type: "task", Action: "update", ID: "t1"}

	// 3 failures
	for i := 0; i < 3; i++ {
		n.HandleEvent(event, "web.1")
		time.Sleep(50 * time.Millisecond)
	}

	// Then a success
	n.HandleEvent(event, "web.1")
	time.Sleep(50 * time.Millisecond)

	// Circuit should be reset
	statuses := n.RuleStatuses()
	if statuses[0].CircuitOpen {
		t.Error("expected CircuitOpen=false after success")
	}
	if statuses[0].ConsecFailures != 0 {
		t.Errorf("expected ConsecFailures=0 after success, got %d", statuses[0].ConsecFailures)
	}
}

func TestNotifier_RuleStatuses(t *testing.T) {
	rules := []Rule{
		{ID: "r1", Name: "rule one", Enabled: true, Match: Match{Type: "task"}, Webhook: "http://example.com"},
		{ID: "r2", Name: "rule two", Enabled: false, Match: Match{Type: "node"}, Webhook: "http://example.com"},
	}
	for i := range rules {
		if err := rules[i].compile(); err != nil {
			t.Fatal(err)
		}
	}

	n := New(rules)
	statuses := n.RuleStatuses()

	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	for _, s := range statuses {
		if !s.LastFired.IsZero() {
			t.Errorf("expected zero LastFired for rule %s before any events", s.ID)
		}
		if s.FireCount != 0 {
			t.Errorf("expected zero FireCount for rule %s before any events", s.ID)
		}
	}

	// Verify fields match rules
	found := make(map[string]RuleStatus)
	for _, s := range statuses {
		found[s.ID] = s
	}
	if s := found["r1"]; s.Name != "rule one" || !s.Enabled {
		t.Errorf("unexpected status for r1: %+v", s)
	}
	if s := found["r2"]; s.Name != "rule two" || s.Enabled {
		t.Errorf("unexpected status for r2: %+v", s)
	}
}
