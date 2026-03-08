# Notification Webhooks Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a file-configured notification system that watches cache events and fires webhooks when rules match (e.g., task failed, node went down).

**Architecture:** A `Notifier` goroutine subscribes to cache events via a second `OnChangeFunc`. It evaluates events against rules loaded from a JSON config file. Matching rules fire async HTTP POST webhooks with cooldown to suppress duplicates. A read-only API endpoint exposes rule state.

**Tech Stack:** Go stdlib (`net/http`, `regexp`, `encoding/json`, `log/slog`, `time`), no new dependencies.

---

### Task 1: Notification Rule Types

**Files:**
- Create: `internal/notify/rule.go`
- Create: `internal/notify/rule_test.go`

**Step 1: Write the failing tests**

```go
// internal/notify/rule_test.go
package notify

import (
	"testing"

	"cetacean/internal/cache"
)

func TestRule_Matches_TypeAndAction(t *testing.T) {
	r := Rule{
		ID: "test", Enabled: true,
		Match: Match{Type: "node", Action: "update"},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	got := r.matches(cache.Event{Type: "node", Action: "update", ID: "n1"}, "worker-01")
	if !got {
		t.Error("expected match")
	}

	got = r.matches(cache.Event{Type: "service", Action: "update", ID: "s1"}, "nginx")
	if got {
		t.Error("expected no match for wrong type")
	}
}

func TestRule_Matches_NameRegex(t *testing.T) {
	r := Rule{
		ID: "test", Enabled: true,
		Match: Match{Type: "service", NameRegex: "^nginx"},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	if !r.matches(cache.Event{Type: "service", Action: "update"}, "nginx-web") {
		t.Error("expected match")
	}
	if r.matches(cache.Event{Type: "service", Action: "update"}, "redis") {
		t.Error("expected no match")
	}
}

func TestRule_Matches_Condition(t *testing.T) {
	r := Rule{
		ID: "test", Enabled: true,
		Match: Match{Type: "task", Condition: "state == failed"},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	task := swarm.Task{Status: swarm.TaskStatus{State: swarm.TaskStateFailed}}
	got := r.matchesCondition(task)
	if !got {
		t.Error("expected condition match")
	}

	task.Status.State = swarm.TaskStateRunning
	if r.matchesCondition(task) {
		t.Error("expected no match for running task")
	}
}

func TestRule_Matches_Disabled(t *testing.T) {
	r := Rule{
		ID: "test", Enabled: false,
		Match: Match{Type: "node"},
	}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}

	got := r.matches(cache.Event{Type: "node", Action: "update"}, "worker-01")
	if got {
		t.Error("disabled rule should not match")
	}
}

func TestRule_Compile_BadRegex(t *testing.T) {
	r := Rule{Match: Match{NameRegex: "[invalid"}}
	if err := r.compile(); err == nil {
		t.Error("expected compile error for bad regex")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/notify/ -run TestRule -v`
Expected: FAIL — package doesn't exist

**Step 3: Write implementation**

```go
// internal/notify/rule.go
package notify

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"cetacean/internal/cache"
)

type Rule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Match    Match  `json:"match"`
	Webhook  string `json:"webhook"`
	Cooldown string `json:"cooldown"`

	// compiled state (not serialized)
	nameRe     *regexp.Regexp
	cooldownDur time.Duration
}

type Match struct {
	Type      string `json:"type,omitempty"`
	Action    string `json:"action,omitempty"`
	NameRegex string `json:"nameRegex,omitempty"`
	Condition string `json:"condition,omitempty"`
}

func (r *Rule) compile() error {
	if r.Match.NameRegex != "" {
		re, err := regexp.Compile(r.Match.NameRegex)
		if err != nil {
			return fmt.Errorf("rule %s: invalid nameRegex: %w", r.ID, err)
		}
		r.nameRe = re
	}
	if r.Cooldown != "" {
		d, err := time.ParseDuration(r.Cooldown)
		if err != nil {
			return fmt.Errorf("rule %s: invalid cooldown: %w", r.ID, err)
		}
		r.cooldownDur = d
	}
	return nil
}

func (r *Rule) matches(e cache.Event, resourceName string) bool {
	if !r.Enabled {
		return false
	}
	if r.Match.Type != "" && r.Match.Type != e.Type {
		return false
	}
	if r.Match.Action != "" && r.Match.Action != e.Action {
		return false
	}
	if r.nameRe != nil && !r.nameRe.MatchString(resourceName) {
		return false
	}
	if r.Match.Condition != "" && !r.matchesCondition(e.Resource) {
		return false
	}
	return true
}

func (r *Rule) matchesCondition(resource interface{}) bool {
	// Parse "field == value" format
	parts := strings.SplitN(r.Match.Condition, "==", 2)
	if len(parts) != 2 {
		return false
	}
	field := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	switch field {
	case "state":
		return extractState(resource) == value
	default:
		return false
	}
}

func extractState(resource interface{}) string {
	switch r := resource.(type) {
	case swarm.Task:
		return string(r.Status.State)
	case swarm.Node:
		return string(r.Status.State)
	default:
		return ""
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/notify/ -run TestRule -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/notify/rule.go internal/notify/rule_test.go
git commit -m "feat: notification rule types with matching and condition evaluation"
```

---

### Task 2: Rule Loading from JSON File

**Files:**
- Create: `internal/notify/config.go`
- Create: `internal/notify/config_test.go`

**Step 1: Write the failing tests**

```go
// internal/notify/config_test.go
package notify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notifications.json")
	data := `[
		{"id":"node-down","name":"Node down","enabled":true,"match":{"type":"node","condition":"state == down"},"webhook":"https://example.com/hook","cooldown":"5m"},
		{"id":"task-failed","name":"Task failed","enabled":true,"match":{"type":"task","condition":"state == failed","nameRegex":"^web"},"webhook":"https://example.com/hook2","cooldown":"1m"}
	]`
	os.WriteFile(path, []byte(data), 0644)

	rules, err := LoadRules(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(rules))
	}
	if rules[0].ID != "node-down" {
		t.Errorf("first rule id=%s, want node-down", rules[0].ID)
	}
	if rules[1].nameRe == nil {
		t.Error("expected compiled regex for second rule")
	}
	if rules[0].cooldownDur.Minutes() != 5 {
		t.Errorf("cooldown=%v, want 5m", rules[0].cooldownDur)
	}
}

func TestLoadRules_FileNotFound(t *testing.T) {
	rules, err := LoadRules("/nonexistent/path.json")
	if err != nil {
		t.Fatal("missing file should return empty rules, not error")
	}
	if len(rules) != 0 {
		t.Errorf("got %d rules, want 0", len(rules))
	}
}

func TestLoadRules_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("{not json"), 0644)

	_, err := LoadRules(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadRules_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-regex.json")
	os.WriteFile(path, []byte(`[{"id":"x","enabled":true,"match":{"nameRegex":"[bad"},"webhook":"http://x"}]`), 0644)

	_, err := LoadRules(path)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/notify/ -run TestLoadRules -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/notify/config.go
package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

func LoadRules(path string) ([]Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read notifications file: %w", err)
	}

	var rules []Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("parse notifications file: %w", err)
	}

	for i := range rules {
		if err := rules[i].compile(); err != nil {
			return nil, err
		}
	}

	return rules, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/notify/ -run TestLoadRules -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/notify/config.go internal/notify/config_test.go
git commit -m "feat: load notification rules from JSON config file"
```

---

### Task 3: Notifier with Webhook Delivery and Cooldown

**Files:**
- Create: `internal/notify/notifier.go`
- Create: `internal/notify/notifier_test.go`

**Step 1: Write the failing tests**

```go
// internal/notify/notifier_test.go
package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"cetacean/internal/cache"
)

func TestNotifier_FiresWebhook(t *testing.T) {
	var called atomic.Int32
	var payload WebhookPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rules := []Rule{{
		ID: "task-failed", Name: "Task failed", Enabled: true,
		Match:   Match{Type: "task", Condition: "state == failed"},
		Webhook: srv.URL,
	}}
	rules[0].compile()

	n := New(rules)
	n.HandleEvent(cache.Event{
		Type: "task", Action: "update", ID: "t1",
		Resource: swarm.Task{Status: swarm.TaskStatus{State: swarm.TaskStateFailed}},
	}, "web.3")

	// Wait for async delivery
	time.Sleep(100 * time.Millisecond)

	if called.Load() != 1 {
		t.Errorf("webhook called %d times, want 1", called.Load())
	}
	if payload.Rule != "task-failed" {
		t.Errorf("payload rule=%s, want task-failed", payload.Rule)
	}
}

func TestNotifier_RespectsCooldow(t *testing.T) {
	var called atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rules := []Rule{{
		ID: "test", Enabled: true,
		Match:   Match{Type: "task", Condition: "state == failed"},
		Webhook: srv.URL,
		Cooldown: "10s",
	}}
	rules[0].compile()

	n := New(rules)

	event := cache.Event{
		Type: "task", Action: "update", ID: "t1",
		Resource: swarm.Task{Status: swarm.TaskStatus{State: swarm.TaskStateFailed}},
	}

	n.HandleEvent(event, "web.1")
	n.HandleEvent(event, "web.1") // should be suppressed by cooldown
	n.HandleEvent(event, "web.1") // should be suppressed by cooldown

	time.Sleep(100 * time.Millisecond)

	if called.Load() != 1 {
		t.Errorf("webhook called %d times, want 1 (cooldown should suppress)", called.Load())
	}
}

func TestNotifier_NoMatchNoFire(t *testing.T) {
	var called atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rules := []Rule{{
		ID: "test", Enabled: true,
		Match:   Match{Type: "node"},
		Webhook: srv.URL,
	}}
	rules[0].compile()

	n := New(rules)
	n.HandleEvent(cache.Event{Type: "service", Action: "update", ID: "s1"}, "nginx")

	time.Sleep(100 * time.Millisecond)

	if called.Load() != 0 {
		t.Errorf("webhook called %d times, want 0", called.Load())
	}
}

func TestNotifier_RuleStatus(t *testing.T) {
	rules := []Rule{{
		ID: "test", Name: "Test rule", Enabled: true,
		Match:   Match{Type: "task"},
		Webhook: "http://example.com",
	}}
	rules[0].compile()

	n := New(rules)
	statuses := n.RuleStatuses()

	if len(statuses) != 1 {
		t.Fatalf("got %d statuses, want 1", len(statuses))
	}
	if statuses[0].ID != "test" {
		t.Errorf("id=%s, want test", statuses[0].ID)
	}
	if !statuses[0].LastFired.IsZero() {
		t.Error("last fired should be zero before any events")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/notify/ -run TestNotifier -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/notify/notifier.go
package notify

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"cetacean/internal/cache"
)

type WebhookPayload struct {
	Rule      string    `json:"rule"`
	Timestamp time.Time `json:"timestamp"`
	Event     EventInfo `json:"event"`
	Message   string    `json:"message"`
}

type EventInfo struct {
	Type       string `json:"type"`
	Action     string `json:"action"`
	ResourceID string `json:"resourceId"`
	Name       string `json:"name"`
}

type RuleStatus struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	LastFired time.Time `json:"lastFired,omitempty"`
	FireCount int       `json:"fireCount"`
}

type Notifier struct {
	rules   []Rule
	mu      sync.RWMutex
	lastFire map[string]time.Time // rule ID -> last fire time
	fireCounts map[string]int
	client  *http.Client
}

func New(rules []Rule) *Notifier {
	return &Notifier{
		rules:      rules,
		lastFire:   make(map[string]time.Time),
		fireCounts: make(map[string]int),
		client:     &http.Client{Timeout: 5 * time.Second},
	}
}

func (n *Notifier) HandleEvent(e cache.Event, resourceName string) {
	for i := range n.rules {
		rule := &n.rules[i]
		if !rule.matches(e, resourceName) {
			continue
		}
		if !n.checkCooldown(rule) {
			continue
		}
		n.recordFire(rule.ID)
		go n.fire(rule, e, resourceName)
	}
}

func (n *Notifier) checkCooldown(rule *Rule) bool {
	if rule.cooldownDur == 0 {
		return true
	}
	n.mu.RLock()
	last, ok := n.lastFire[rule.ID]
	n.mu.RUnlock()
	if !ok {
		return true
	}
	return time.Since(last) >= rule.cooldownDur
}

func (n *Notifier) recordFire(ruleID string) {
	n.mu.Lock()
	n.lastFire[ruleID] = time.Now()
	n.fireCounts[ruleID]++
	n.mu.Unlock()
}

func (n *Notifier) fire(rule *Rule, e cache.Event, resourceName string) {
	payload := WebhookPayload{
		Rule:      rule.ID,
		Timestamp: time.Now(),
		Event: EventInfo{
			Type:       e.Type,
			Action:     e.Action,
			ResourceID: e.ID,
			Name:       resourceName,
		},
		Message: rule.Name + ": " + resourceName,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("notification marshal failed", "rule", rule.ID, "error", err)
		return
	}

	resp, err := n.client.Post(rule.Webhook, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Warn("notification webhook failed", "rule", rule.ID, "error", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("notification webhook returned error", "rule", rule.ID, "status", resp.StatusCode)
	}
}

func (n *Notifier) RuleStatuses() []RuleStatus {
	n.mu.RLock()
	defer n.mu.RUnlock()

	statuses := make([]RuleStatus, len(n.rules))
	for i, rule := range n.rules {
		statuses[i] = RuleStatus{
			ID:        rule.ID,
			Name:      rule.Name,
			Enabled:   rule.Enabled,
			LastFired: n.lastFire[rule.ID],
			FireCount: n.fireCounts[rule.ID],
		}
	}
	return statuses
}
```

**Step 4: Run tests**

Run: `go test ./internal/notify/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/notify/notifier.go internal/notify/notifier_test.go
git commit -m "feat: notifier with webhook delivery and cooldown"
```

---

### Task 4: Wire Notifier into Cache and Add API Endpoint

**Files:**
- Modify: `internal/config/config.go`
- Modify: `main.go`
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/router.go`

**Step 1: Add `CETACEAN_NOTIFICATIONS_FILE` to config**

In `internal/config/config.go`, add a `NotificationsFile` field to `Config`:

```go
type Config struct {
	// ... existing fields ...
	NotificationsFile string // CETACEAN_NOTIFICATIONS_FILE, optional
}
```

In `Load()`, add:
```go
NotificationsFile: os.Getenv("CETACEAN_NOTIFICATIONS_FILE"),
```

**Step 2: Wire notifier in `main.go`**

After creating the state cache and before the watcher:

```go
// Notifications (optional)
var notifier *notify.Notifier
if cfg.NotificationsFile != "" {
	rules, err := notify.LoadRules(cfg.NotificationsFile)
	if err != nil {
		slog.Error("failed to load notification rules", "error", err)
		os.Exit(1)
	}
	slog.Info("loaded notification rules", "count", len(rules))
	notifier = notify.New(rules)
}
```

Update the cache `OnChangeFunc` to also call the notifier:

```go
stateCache := cache.New(func(e cache.Event) {
	broadcaster.Broadcast(e)
	if notifier != nil {
		notifier.HandleEvent(e, cache.ExtractName(e))
	}
})
```

This requires exporting `extractName` from `cache.go` as `ExtractName`.

**Step 3: Export `extractName` in `cache.go`**

Rename `extractName` to `ExtractName` in `internal/cache/cache.go`.

**Step 4: Add notification rules API endpoint**

In `internal/api/handlers.go`:

```go
func (h *Handlers) HandleNotificationRules(w http.ResponseWriter, r *http.Request) {
	if h.notifier == nil {
		writeJSON(w, []struct{}{})
		return
	}
	writeJSON(w, h.notifier.RuleStatuses())
}
```

Add `notifier` field to `Handlers` struct (as an interface to avoid import cycle):

```go
type NotificationStatusProvider interface {
	RuleStatuses() []notify.RuleStatus
}
```

Actually, to avoid an import cycle, define the interface in the `api` package and have the notifier satisfy it. Or pass the `RuleStatuses` function directly. Simplest: pass `notifier` as an optional parameter to `NewHandlers`.

Update `NewHandlers`:
```go
func NewHandlers(c *cache.Cache, dc DockerLogStreamer, ready <-chan struct{}, notifier *notify.Notifier) *Handlers {
```

In `router.go`, add:
```go
mux.HandleFunc("GET /api/notifications/rules", h.HandleNotificationRules)
```

**Step 5: Write test for the endpoint**

```go
func TestHandleNotificationRules_Empty(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, make(chan struct{}), nil)

	req := httptest.NewRequest("GET", "/api/notifications/rules", nil)
	w := httptest.NewRecorder()
	h.HandleNotificationRules(w, req)

	if w.Code != 200 {
		t.Errorf("status=%d, want 200", w.Code)
	}
}
```

**Step 6: Run all tests**

Run: `go test ./... -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/config/config.go internal/cache/cache.go internal/api/handlers.go internal/api/router.go internal/notify/ main.go
git commit -m "feat: wire notification webhooks into cache events and API"
```

---

### Task 5: Frontend — Notification Rules Status Display

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`
- Create: `frontend/src/components/NotificationRules.tsx`
- Modify: `frontend/src/pages/ClusterOverview.tsx`

**Step 1: Add types and API endpoint**

```typescript
// types.ts
export interface NotificationRuleStatus {
  id: string;
  name: string;
  enabled: boolean;
  lastFired?: string;
  fireCount: number;
}
```

```typescript
// client.ts
notificationRules: () => fetchJSON<NotificationRuleStatus[]>("/notifications/rules"),
```

**Step 2: Create NotificationRules component**

A small card showing configured rules and their fire status. Only shown when rules exist (non-empty array).

Each rule shows: name, enabled badge, fire count, last fired time (relative via TimeAgo). Compact table layout consistent with the ActivityFeed styling.

**Step 3: Add to ClusterOverview**

Fetch notification rules on mount. If rules exist, render the component below the Activity Feed.

**Step 4: Run checks**

Run: `cd frontend && npx tsc --noEmit && npm run lint`

**Step 5: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts frontend/src/components/NotificationRules.tsx frontend/src/pages/ClusterOverview.tsx
git commit -m "feat: notification rules status display on cluster overview"
```

---

## Integration Checklist

After all tasks:
1. `go test ./...`
2. `cd frontend && npx tsc --noEmit && npm run lint`
3. Test manually: create a `notifications.json` with a rule, set `CETACEAN_NOTIFICATIONS_FILE`, verify webhook fires when matching events occur
4. `make check`
