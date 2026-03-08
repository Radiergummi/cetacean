package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	rules      []Rule
	mu         sync.RWMutex
	lastFire   map[string]time.Time
	fireCounts map[string]int
	client     *http.Client
}

func New(rules []Rule) *Notifier {
	return &Notifier{
		rules:      rules,
		lastFire:   make(map[string]time.Time),
		fireCounts: make(map[string]int),
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *Notifier) HandleEvent(e cache.Event, resourceName string) {
	for i := range n.rules {
		r := &n.rules[i]
		if r.matches(e, resourceName) && n.checkCooldown(r) {
			n.recordFire(r.ID)
			go n.fire(r, e, resourceName)
		}
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
		slog.Error("notify: webhook request", "rule", rule.ID, "url", rule.Webhook, "error", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Error("notify: webhook response", "rule", rule.ID, "status", resp.StatusCode)
	}
}

func (n *Notifier) RuleStatuses() []RuleStatus {
	n.mu.RLock()
	defer n.mu.RUnlock()

	statuses := make([]RuleStatus, len(n.rules))
	for i, r := range n.rules {
		statuses[i] = RuleStatus{
			ID:        r.ID,
			Name:      r.Name,
			Enabled:   r.Enabled,
			LastFired: n.lastFire[r.ID],
			FireCount: n.fireCounts[r.ID],
		}
	}
	return statuses
}
