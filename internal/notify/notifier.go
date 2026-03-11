package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Enabled        bool      `json:"enabled"`
	LastFired      time.Time `json:"lastFired,omitempty"`
	FireCount      int       `json:"fireCount"`
	CircuitOpen    bool      `json:"circuitOpen"`
	ConsecFailures int       `json:"consecFailures"`
}

const (
	circuitThreshold = 5                // consecutive failures before opening
	circuitTimeout   = 30 * time.Second // how long the circuit stays open
)

type circuitState struct {
	failures int
	openedAt time.Time
	halfOpen bool // true while a probe is in flight
}

type Notifier struct {
	rules      []Rule
	mu         sync.RWMutex
	lastFire   map[string]time.Time
	fireCounts map[string]int
	circuits   map[string]*circuitState
	client     *http.Client
	wg         sync.WaitGroup
}

func New(rules []Rule) *Notifier {
	return &Notifier{
		rules:      rules,
		lastFire:   make(map[string]time.Time),
		fireCounts: make(map[string]int),
		circuits:   make(map[string]*circuitState),
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *Notifier) HandleEvent(e cache.Event, resourceName string) {
	// n.rules is immutable after construction — no lock needed.
	for i := range n.rules {
		r := &n.rules[i]
		if r.matches(e, resourceName) && n.checkAndRecordFire(r) {
			n.wg.Add(1)
			go func() {
				defer n.wg.Done()
				n.fire(r, e, resourceName)
			}()
		}
	}
}

// Close waits for all in-flight webhook calls to complete.
func (n *Notifier) Close() {
	n.wg.Wait()
}

func (n *Notifier) recordSuccess(ruleID string) {
	n.mu.Lock()
	delete(n.circuits, ruleID)
	n.mu.Unlock()
}

func (n *Notifier) recordFailure(ruleID string) {
	n.mu.Lock()
	cs, ok := n.circuits[ruleID]
	if !ok {
		cs = &circuitState{}
		n.circuits[ruleID] = cs
	}
	cs.failures++
	if cs.halfOpen {
		// Half-open probe failed — reset so the circuit re-enters open state
		// with a fresh timeout, allowing another probe after circuitTimeout.
		cs.halfOpen = false
		cs.openedAt = time.Now()
	} else if cs.failures == circuitThreshold {
		cs.openedAt = time.Now()
	}
	n.mu.Unlock()
}

// checkAndRecordFire atomically checks the circuit breaker, cooldown, and records
// the fire time under a single write lock to prevent TOCTOU races on half-open transitions.
func (n *Notifier) checkAndRecordFire(rule *Rule) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	// Circuit breaker check
	if cs, ok := n.circuits[rule.ID]; ok && cs.failures >= circuitThreshold {
		if time.Since(cs.openedAt) < circuitTimeout {
			return false
		}
		// Half-open: allow one probe at a time.
		if cs.halfOpen {
			return false
		}
		cs.halfOpen = true
	}
	// Cooldown check
	if rule.cooldownDur != 0 {
		if last, ok := n.lastFire[rule.ID]; ok && time.Since(last) < rule.cooldownDur {
			return false
		}
	}
	n.lastFire[rule.ID] = time.Now()
	n.fireCounts[rule.ID]++
	return true
}

func (n *Notifier) fire(rule *Rule, e cache.Event, resourceName string) {
	payload := WebhookPayload{
		Rule:      rule.Name,
		Timestamp: time.Now(),
		Event: EventInfo{
			Type:       e.Type,
			Action:     e.Action,
			ResourceID: e.ID,
			Name:       resourceName,
		},
		Message: fmt.Sprintf("[%s] %s %s: %s", rule.Name, e.Type, e.Action, resourceName),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("notify: marshal payload", "rule", rule.ID, "error", err)
		return
	}

	resp, err := n.client.Post(rule.Webhook, "application/json", bytes.NewReader(body))
	if err != nil {
		n.recordFailure(rule.ID)
		slog.Error("notify: webhook request", "rule", rule.ID, "url", rule.Webhook, "error", err)
		return
	}
	defer resp.Body.Close()
	// Drain body to allow connection reuse and bound memory from large responses.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= 400 {
		n.recordFailure(rule.ID)
		slog.Error("notify: webhook response", "rule", rule.ID, "status", resp.StatusCode)
		return
	}
	n.recordSuccess(rule.ID)
}

func (n *Notifier) RuleStatuses() []RuleStatus {
	n.mu.RLock()
	defer n.mu.RUnlock()

	statuses := make([]RuleStatus, len(n.rules))
	for i, r := range n.rules {
		s := RuleStatus{
			ID:        r.ID,
			Name:      r.Name,
			Enabled:   r.Enabled,
			LastFired: n.lastFire[r.ID],
			FireCount: n.fireCounts[r.ID],
		}
		if cs, ok := n.circuits[r.ID]; ok {
			s.ConsecFailures = cs.failures
			s.CircuitOpen = cs.failures >= circuitThreshold && time.Since(cs.openedAt) < circuitTimeout
		}
		statuses[i] = s
	}
	return statuses
}
