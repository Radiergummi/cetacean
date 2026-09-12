# Alertmanager Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Alertmanager integration to Cetacean — alert viewing with cross-referencing to Swarm resources, silence management via Alertmanager API, real-time alert updates via webhooks + SSE, and a predefined Prometheus rules file.

**Architecture:** Cetacean receives alert state changes from Alertmanager via webhook (`POST /-/webhooks/alertmanager`), caches them in-memory with cross-references to services/nodes/stacks, and streams them to browsers via the existing SSE broadcaster. Silences are proxied to Alertmanager on-demand (not cached). A new `/alerting/` route prefix hosts alert and silence endpoints. Configuration uses auto-discovery from Prometheus with optional explicit URL override.

**Tech Stack:** Go stdlib HTTP, Alertmanager v2 API, existing cache/SSE/ACL infrastructure, React 19 + TanStack Query + shadcn/ui

---

## File Structure

### Backend — New files
- `internal/alertmanager/client.go` — Alertmanager v2 API HTTP client
- `internal/alertmanager/client_test.go` — Client tests with httptest server
- `internal/alertmanager/types.go` — Alert, Silence, AlertGroup, Matcher types (matching AM v2 API)
- `internal/alertmanager/discover.go` — Auto-discovery via Prometheus `/api/v1/alertmanagers`
- `internal/alertmanager/discover_test.go` — Discovery tests
- `internal/alertmanager/webhook.go` — Webhook handler + secret validation
- `internal/alertmanager/webhook_test.go` — Webhook handler tests
- `internal/api/alert_handlers.go` — Alert list/detail/groups API handlers
- `internal/api/alert_handlers_test.go` — Alert handler tests
- `internal/api/silence_handlers.go` — Silence list/create/expire handlers
- `internal/api/silence_handlers_test.go` — Silence handler tests
- `frontend/src/pages/AlertList.tsx` — Alert list page
- `frontend/src/pages/AlertDetail.tsx` — Alert detail page
- `frontend/src/pages/SilenceList.tsx` — Silence list page
- `frontend/src/components/SilenceForm.tsx` — Silence creation dialog
- `frontend/src/components/AlertBadge.tsx` — Firing alert count badge
- `rules/cetacean.rules.yml` — Predefined recording + alerting rules

### Backend — Modified files
- `internal/config/config.go` — Add `AlertmanagerURL`, `AlertmanagerWebhookSecret` fields
- `internal/config/file.go` — Add `fileAlertmanager` TOML struct
- `internal/cache/cache.go` — Add `EventAlert` constant, `alerts` map, alert methods, cross-reference lookups
- `internal/api/handlers.go` — Add `alertmanagerClient` field to Handlers, update `NewHandlers`
- `internal/api/router.go` — Register `/alerting/` routes and webhook endpoint
- `internal/api/cluster_handlers.go` — Extend `MonitoringStatus` with Alertmanager fields
- `internal/api/search_handlers.go` — Include alerts in global search
- `main.go` — Initialize Alertmanager client, wire webhook handler

### Frontend — Modified files
- `frontend/src/api/types.ts` — Add Alert, Silence types; extend MonitoringStatus, SearchResourceType
- `frontend/src/api/client.ts` — Add alert/silence API methods
- `frontend/src/hooks/useSwarmQuery.ts` — Add `alert` to ssePathMap
- `frontend/src/hooks/useMonitoringStatus.ts` — Add `isAlertmanagerReady` helper
- `frontend/src/lib/searchConstants.ts` — Add alerts to type order/labels/path
- `frontend/src/App.tsx` — Add routes, nav link, keyboard shortcut, lazy imports
- `frontend/src/components/metrics/MonitoringStatus.tsx` — Add Alertmanager tier

---

## Task 1: Alertmanager Types

**Files:**
- Create: `internal/alertmanager/types.go`

- [ ] **Step 1: Create the types file**

```go
package alertmanager

import "time"

// Alert represents an Alertmanager v2 alert.
type Alert struct {
	Fingerprint  string            `json:"fingerprint"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	GeneratorURL string            `json:"generatorURL"`
	Status       AlertStatus       `json:"status"`
}

// AlertStatus represents the status of an alert.
type AlertStatus struct {
	State       string   `json:"state"` // "active", "suppressed", "unprocessed"
	SilencedBy  []string `json:"silencedBy"`
	InhibitedBy []string `json:"inhibitedBy"`
}

// AlertGroup represents a group of alerts from Alertmanager.
type AlertGroup struct {
	Labels   map[string]string `json:"labels"`
	Receiver Receiver          `json:"receiver"`
	Alerts   []Alert           `json:"alerts"`
}

// Receiver identifies an Alertmanager receiver.
type Receiver struct {
	Name string `json:"name"`
}

// Silence represents an Alertmanager v2 silence.
type Silence struct {
	ID        string    `json:"id"`
	Matchers  []Matcher `json:"matchers"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	CreatedBy string    `json:"createdBy"`
	Comment   string    `json:"comment"`
	Status    SilenceStatus `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SilenceStatus holds the state of a silence.
type SilenceStatus struct {
	State string `json:"state"` // "active", "pending", "expired"
}

// Matcher is a label matcher for silences.
type Matcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual *bool  `json:"isEqual,omitempty"` // nil = true (equal match)
}

// CreateSilenceRequest is the payload for creating a new silence.
type CreateSilenceRequest struct {
	Matchers  []Matcher `json:"matchers"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	CreatedBy string    `json:"createdBy"`
	Comment   string    `json:"comment"`
}

// WebhookPayload is the payload Alertmanager sends to webhook receivers.
type WebhookPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
	Status            string            `json:"status"` // "firing" or "resolved"
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []WebhookAlert    `json:"alerts"`
}

// WebhookAlert is a single alert within a webhook payload.
type WebhookAlert struct {
	Status       string            `json:"status"` // "firing" or "resolved"
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/alertmanager/`
Expected: success (no output)

- [ ] **Step 3: Commit**

```bash
git add internal/alertmanager/types.go
git commit -m "feat(alertmanager): add Alertmanager v2 API types"
```

---

## Task 2: Alertmanager Client

**Files:**
- Create: `internal/alertmanager/client_test.go`
- Create: `internal/alertmanager/client.go`

- [ ] **Step 1: Write tests for the client**

```go
package alertmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_GetAlerts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/alerts" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{
			"fingerprint": "abc123",
			"labels": {"alertname": "HighCPU", "severity": "warning"},
			"annotations": {"summary": "CPU is high"},
			"startsAt": "2026-04-05T10:00:00Z",
			"endsAt": "0001-01-01T00:00:00Z",
			"updatedAt": "2026-04-05T10:00:00Z",
			"generatorURL": "http://prometheus:9090/graph",
			"status": {"state": "active", "silencedBy": [], "inhibitedBy": []}
		}]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	alerts, err := c.GetAlerts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Fingerprint != "abc123" {
		t.Errorf("expected fingerprint abc123, got %s", alerts[0].Fingerprint)
	}
	if alerts[0].Labels["alertname"] != "HighCPU" {
		t.Errorf("expected alertname HighCPU, got %s", alerts[0].Labels["alertname"])
	}
}

func TestClient_GetAlerts_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.GetAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestClient_GetSilences(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/silences" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{
			"id": "silence-1",
			"matchers": [{"name": "alertname", "value": "HighCPU", "isRegex": false}],
			"startsAt": "2026-04-05T10:00:00Z",
			"endsAt": "2026-04-05T12:00:00Z",
			"createdBy": "admin",
			"comment": "maintenance window",
			"status": {"state": "active"},
			"updatedAt": "2026-04-05T10:00:00Z"
		}]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	silences, err := c.GetSilences(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(silences) != 1 {
		t.Fatalf("expected 1 silence, got %d", len(silences))
	}
	if silences[0].ID != "silence-1" {
		t.Errorf("expected id silence-1, got %s", silences[0].ID)
	}
}

func TestClient_CreateSilence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/silences" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"silenceID": "new-silence-1"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	id, err := c.CreateSilence(context.Background(), CreateSilenceRequest{
		Matchers:  []Matcher{{Name: "alertname", Value: "HighCPU"}},
		StartsAt:  time.Now(),
		EndsAt:    time.Now().Add(2 * time.Hour),
		CreatedBy: "admin",
		Comment:   "maintenance",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "new-silence-1" {
		t.Errorf("expected id new-silence-1, got %s", id)
	}
}

func TestClient_ExpireSilence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/silence/silence-1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.ExpireSilence(context.Background(), "silence-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_GetStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/status" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cluster":{"status":"ready"},"versionInfo":{"version":"0.27.0"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/alertmanager/ -v -run TestClient`
Expected: compilation error — `NewClient` not defined

- [ ] **Step 3: Implement the client**

```go
package alertmanager

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

// Client talks to the Alertmanager v2 HTTP API.
type Client struct {
	baseURL string
	client  *http.Client
}

// NewClient creates an Alertmanager API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// URL returns the base URL of the Alertmanager instance.
func (c *Client) URL() string { return c.baseURL }

// GetAlerts returns all alerts from Alertmanager.
func (c *Client) GetAlerts(ctx context.Context) ([]Alert, error) {
	var alerts []Alert
	if err := c.get(ctx, "/api/v2/alerts", &alerts); err != nil {
		return nil, fmt.Errorf("get alerts: %w", err)
	}
	return alerts, nil
}

// GetAlertGroups returns alerts grouped by Alertmanager's routing tree.
func (c *Client) GetAlertGroups(ctx context.Context) ([]AlertGroup, error) {
	var groups []AlertGroup
	if err := c.get(ctx, "/api/v2/alerts/groups", &groups); err != nil {
		return nil, fmt.Errorf("get alert groups: %w", err)
	}
	return groups, nil
}

// GetSilences returns all silences from Alertmanager.
func (c *Client) GetSilences(ctx context.Context) ([]Silence, error) {
	var silences []Silence
	if err := c.get(ctx, "/api/v2/silences", &silences); err != nil {
		return nil, fmt.Errorf("get silences: %w", err)
	}
	return silences, nil
}

// CreateSilence creates a new silence and returns its ID.
func (c *Client) CreateSilence(ctx context.Context, req CreateSilenceRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal silence request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v2/silences", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("create silence: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", c.readError(resp)
	}

	var result struct {
		SilenceID string `json:"silenceID"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("decode create silence response: %w", err)
	}
	return result.SilenceID, nil
}

// ExpireSilence expires (deletes) a silence by ID.
func (c *Client) ExpireSilence(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/v2/silence/"+id, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("expire silence: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// GetStatus checks that Alertmanager is reachable.
func (c *Client) GetStatus(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v2/status", nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("alertmanager status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}

	return json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(dest)
}

func (c *Client) readError(resp *http.Response) error {
	preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("alertmanager returned HTTP %d: %s", resp.StatusCode, string(preview))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/alertmanager/ -v -run TestClient`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/alertmanager/client.go internal/alertmanager/client_test.go
git commit -m "feat(alertmanager): add Alertmanager v2 API client"
```

---

## Task 3: Auto-discovery

**Files:**
- Create: `internal/alertmanager/discover_test.go`
- Create: `internal/alertmanager/discover.go`

- [ ] **Step 1: Write tests for discovery**

```go
package alertmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscover_ReturnsFirstActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/alertmanagers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"status": "success",
			"data": {
				"activeAlertmanagers": [
					{"url": "http://alertmanager:9093/api/v2/alerts"}
				],
				"droppedAlertmanagers": []
			}
		}`))
	}))
	defer srv.Close()

	url, err := Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "http://alertmanager:9093" {
		t.Errorf("expected http://alertmanager:9093, got %s", url)
	}
}

func TestDiscover_NoActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"status": "success",
			"data": {
				"activeAlertmanagers": [],
				"droppedAlertmanagers": []
			}
		}`))
	}))
	defer srv.Close()

	url, err := Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty string, got %s", url)
	}
}

func TestDiscover_PrometheusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Discover(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/alertmanager/ -v -run TestDiscover`
Expected: compilation error — `Discover` not defined

- [ ] **Step 3: Implement discovery**

```go
package alertmanager

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

// Discover queries Prometheus at prometheusURL for active Alertmanager
// instances. Returns the base URL of the first active instance, or ""
// if none are found.
func Discover(ctx context.Context, prometheusURL string) (string, error) {
	u := strings.TrimRight(prometheusURL, "/") + "/api/v1/alertmanagers"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("discover alertmanagers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("prometheus returned HTTP %d: %s", resp.StatusCode, string(preview))
	}

	var body struct {
		Status string `json:"status"`
		Data   struct {
			ActiveAlertmanagers []struct {
				URL string `json:"url"`
			} `json:"activeAlertmanagers"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("decode alertmanagers response: %w", err)
	}

	if len(body.Data.ActiveAlertmanagers) == 0 {
		return "", nil
	}

	// The URL from Prometheus includes the API path (e.g. ".../api/v2/alerts").
	// Extract just the base URL (scheme + host).
	raw := body.Data.ActiveAlertmanagers[0].URL
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw, nil
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/alertmanager/ -v -run TestDiscover`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/alertmanager/discover.go internal/alertmanager/discover_test.go
git commit -m "feat(alertmanager): add auto-discovery via Prometheus API"
```

---

## Task 4: Configuration

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/file.go`

- [ ] **Step 1: Add TOML struct to file.go**

In `internal/config/file.go`, add the `fileAlertmanager` struct and add it to `fileConfig`:

Add to imports: nothing needed.

Add after `fileACL` struct (around line 162):

```go
type fileAlertmanager struct {
	URL           *string `toml:"url"`
	WebhookSecret *string `toml:"webhook_secret"`
}
```

Add field to `fileConfig` struct (after line 59, the ACL field):

```go
Alertmanager *fileAlertmanager `toml:"alertmanager"`
```

- [ ] **Step 2: Add fields to Config struct and Load function**

In `internal/config/config.go`:

Add to `Config` struct (after `TrustedProxies` field, line 47):

```go
AlertmanagerURL          string // CETACEAN_ALERTMANAGER_URL
AlertmanagerWebhookSecret string // CETACEAN_ALERTMANAGER_WEBHOOK_SECRET
```

In `Load` function, add file config pointer extraction (in the `if fc != nil` block, after the ACL section):

```go
var fAlertmanagerURL *string
var fAlertmanagerWebhookSecret *string
```

And inside `if fc != nil`:

```go
if fc.Alertmanager != nil {
	fAlertmanagerURL = fc.Alertmanager.URL
	fAlertmanagerWebhookSecret = fc.Alertmanager.WebhookSecret
}
```

Add to the `cfg := &Config{` block (before the closing `}`):

```go
AlertmanagerURL: resolve(
	"", // no flag for alertmanager URL
	"CETACEAN_ALERTMANAGER_URL",
	fAlertmanagerURL,
	"",
),
AlertmanagerWebhookSecret: resolve(
	"",
	"CETACEAN_ALERTMANAGER_WEBHOOK_SECRET",
	fAlertmanagerWebhookSecret,
	"",
),
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/config/`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/config/file.go
git commit -m "feat(config): add alertmanager.url and alertmanager.webhook_secret"
```

---

## Task 5: Cache — Alert Storage and Cross-referencing

**Files:**
- Modify: `internal/cache/cache.go`

- [ ] **Step 1: Add EventAlert constant and CachedAlert type**

In `internal/cache/cache.go`, add to the EventType constants (after `EventSync`, line 26):

```go
EventAlert EventType = "alert"
```

Add the `CachedAlert` struct after the `ClusterSnapshot` struct:

```go
// CachedAlert wraps an alert with cross-reference IDs resolved at cache time.
type CachedAlert struct {
	Fingerprint  string            `json:"fingerprint"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	GeneratorURL string            `json:"generatorURL"`
	State        string            `json:"state"`
	SilencedBy   []string          `json:"silencedBy,omitempty"`
	InhibitedBy  []string          `json:"inhibitedBy,omitempty"`

	// Cross-references resolved at cache time.
	ServiceID   string `json:"serviceId,omitempty"`
	ServiceName string `json:"serviceName,omitempty"`
	NodeID      string `json:"nodeId,omitempty"`
	NodeName    string `json:"nodeName,omitempty"`
	StackName   string `json:"stackName,omitempty"`
}
```

- [ ] **Step 2: Add alerts map to Cache struct and constructor**

Add to `Cache` struct (after `stacks` field):

```go
alerts map[string]CachedAlert // fingerprint -> alert
```

Add to `New` constructor:

```go
alerts: make(map[string]CachedAlert),
```

- [ ] **Step 3: Add alert CRUD methods**

Add the following methods to `cache.go`:

```go
func (c *Cache) SetAlert(a CachedAlert) {
	c.mu.Lock()

	// Resolve cross-references.
	a.ServiceID, a.ServiceName = c.resolveAlertService(a.Labels)
	a.NodeID, a.NodeName = c.resolveAlertNode(a.Labels)
	a.StackName = c.resolveAlertStack(a.Labels)

	_, exists := c.alerts[a.Fingerprint]
	c.alerts[a.Fingerprint] = a
	c.mu.Unlock()

	action := "update"
	if !exists {
		action = "add"
	}
	c.notify(Event{Type: EventAlert, Action: action, ID: a.Fingerprint, Resource: a})
}

func (c *Cache) DeleteAlert(fingerprint string) {
	c.mu.Lock()
	a, ok := c.alerts[fingerprint]
	if ok {
		delete(c.alerts, fingerprint)
	}
	c.mu.Unlock()

	if ok {
		c.notify(Event{Type: EventAlert, Action: "remove", ID: fingerprint, Resource: a})
	}
}

func (c *Cache) GetAlert(fingerprint string) (CachedAlert, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	a, ok := c.alerts[fingerprint]
	return a, ok
}

func (c *Cache) ListAlerts() []CachedAlert {
	c.mu.RLock()
	defer c.mu.RUnlock()
	alerts := make([]CachedAlert, 0, len(c.alerts))
	for _, a := range c.alerts {
		alerts = append(alerts, a)
	}
	return alerts
}

func (c *Cache) AlertsForService(serviceID string) []CachedAlert {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var result []CachedAlert
	for _, a := range c.alerts {
		if a.ServiceID == serviceID {
			result = append(result, a)
		}
	}
	return result
}

func (c *Cache) AlertsForNode(nodeID string) []CachedAlert {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var result []CachedAlert
	for _, a := range c.alerts {
		if a.NodeID == nodeID {
			result = append(result, a)
		}
	}
	return result
}

func (c *Cache) AlertsForStack(stackName string) []CachedAlert {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var result []CachedAlert
	for _, a := range c.alerts {
		if a.StackName == stackName {
			result = append(result, a)
		}
	}
	return result
}

func (c *Cache) AlertCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	count := 0
	for _, a := range c.alerts {
		if a.State == "active" {
			count++
		}
	}
	return count
}

// resolveAlertService matches alert labels to a cached service.
func (c *Cache) resolveAlertService(labels map[string]string) (string, string) {
	// Try the standard cAdvisor label first.
	svcName := labels["container_label_com_docker_swarm_service_name"]
	if svcName == "" {
		return "", ""
	}
	for _, svc := range c.services {
		if svc.Spec.Name == svcName {
			return svc.ID, svc.Spec.Name
		}
	}
	return "", svcName
}

// resolveAlertNode matches alert labels to a cached node.
func (c *Cache) resolveAlertNode(labels map[string]string) (string, string) {
	instance := labels["instance"]
	nodeName := labels["node"]
	for _, n := range c.nodes {
		hostname := n.Description.Hostname
		addr := n.Status.Addr
		if hostname == nodeName || hostname == instance || addr == instance {
			return n.ID, hostname
		}
		// instance may include port, e.g. "10.0.0.1:9100"
		if idx := strings.Index(instance, ":"); idx > 0 {
			if addr == instance[:idx] {
				return n.ID, hostname
			}
		}
	}
	return "", ""
}

// resolveAlertStack matches alert labels to a cached stack.
func (c *Cache) resolveAlertStack(labels map[string]string) string {
	stackName := labels["container_label_com_docker_stack_namespace"]
	if stackName != "" {
		return stackName
	}
	return ""
}
```

Note: add `"strings"` to imports if not already present.

- [ ] **Step 4: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/cache/`
Expected: success

- [ ] **Step 5: Commit**

```bash
git add internal/cache/cache.go
git commit -m "feat(cache): add alert storage with cross-referencing to services/nodes/stacks"
```

---

## Task 6: Webhook Handler

**Files:**
- Create: `internal/alertmanager/webhook_test.go`
- Create: `internal/alertmanager/webhook.go`

- [ ] **Step 1: Write webhook handler tests**

```go
package alertmanager

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/goccy/go-json"
)

func TestWebhookHandler_ValidSecret(t *testing.T) {
	var received []WebhookAlert
	handler := NewWebhookHandler("my-secret", func(alerts []WebhookAlert, status string) {
		received = alerts
	})

	payload := WebhookPayload{
		Version:  "4",
		Status:   "firing",
		Receiver: "webhook",
		Alerts: []WebhookAlert{
			{
				Status:      "firing",
				Labels:      map[string]string{"alertname": "HighCPU"},
				Fingerprint: "abc123",
			},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/-/webhooks/alertmanager", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Secret", "my-secret")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(received) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(received))
	}
	if received[0].Fingerprint != "abc123" {
		t.Errorf("expected fingerprint abc123, got %s", received[0].Fingerprint)
	}
}

func TestWebhookHandler_InvalidSecret(t *testing.T) {
	handler := NewWebhookHandler("my-secret", func(alerts []WebhookAlert, status string) {
		t.Fatal("callback should not be called")
	})

	req := httptest.NewRequest(http.MethodPost, "/-/webhooks/alertmanager", nil)
	req.Header.Set("X-Webhook-Secret", "wrong-secret")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestWebhookHandler_NoSecret(t *testing.T) {
	var called bool
	handler := NewWebhookHandler("", func(alerts []WebhookAlert, status string) {
		called = true
	})

	payload := WebhookPayload{
		Version: "4",
		Status:  "firing",
		Alerts:  []WebhookAlert{{Fingerprint: "abc"}},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/-/webhooks/alertmanager", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !called {
		t.Error("expected callback to be called when no secret is configured")
	}
}

func TestWebhookHandler_MethodNotAllowed(t *testing.T) {
	handler := NewWebhookHandler("", func([]WebhookAlert, string) {})

	req := httptest.NewRequest(http.MethodGet, "/-/webhooks/alertmanager", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/alertmanager/ -v -run TestWebhook`
Expected: compilation error — `NewWebhookHandler` not defined

- [ ] **Step 3: Implement webhook handler**

```go
package alertmanager

import (
	"crypto/subtle"
	"io"
	"log/slog"
	"net/http"

	json "github.com/goccy/go-json"
)

// WebhookCallback is called when a valid webhook payload is received.
type WebhookCallback func(alerts []WebhookAlert, status string)

// WebhookHandler handles incoming Alertmanager webhook notifications.
type WebhookHandler struct {
	secret   string
	callback WebhookCallback
}

// NewWebhookHandler creates a webhook handler. If secret is empty,
// no authentication is required.
func NewWebhookHandler(secret string, callback WebhookCallback) *WebhookHandler {
	return &WebhookHandler{secret: secret, callback: callback}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if h.secret != "" {
		got := r.Header.Get("X-Webhook-Secret")
		if subtle.ConstantTimeCompare([]byte(got), []byte(h.secret)) != 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}

	var payload WebhookPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 5<<20)).Decode(&payload); err != nil {
		slog.Warn("alertmanager webhook: invalid payload", "error", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	h.callback(payload.Alerts, payload.Status)
	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/alertmanager/ -v -run TestWebhook`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/alertmanager/webhook.go internal/alertmanager/webhook_test.go
git commit -m "feat(alertmanager): add webhook receiver with secret validation"
```

---

## Task 7: Alert API Handlers

**Files:**
- Create: `internal/api/alert_handlers.go`

- [ ] **Step 1: Create the alert handlers**

```go
package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/alertmanager"
	"github.com/radiergummi/cetacean/internal/cache"
)

func (h *Handlers) HandleListAlerts(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	alerts := h.cache.ListAlerts()

	// ACL filter: only show alerts for resources the user can read.
	alerts = h.filterAlertsByACL(r, alerts)

	sorted := applySortAndSearch(r, alerts, alertSortFields, alertSearchFields)
	writeCollectionResponse(w, r, sorted, "/alerting/alerts", "Alert")
}

func (h *Handlers) HandleGetAlert(w http.ResponseWriter, r *http.Request) {
	fingerprint := r.PathValue("fingerprint")
	alert, ok := h.cache.GetAlert(fingerprint)
	if !ok {
		writeNotFound(w, r, "alert", fingerprint)
		return
	}

	if !h.canReadAlert(r, alert) {
		writeNotFound(w, r, "alert", fingerprint)
		return
	}

	writeDetailResponse(w, r, alert, "/alerting/alerts/"+fingerprint, "Alert", nil)
}

func (h *Handlers) HandleAlertGroups(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	if h.alertmanagerClient == nil {
		writeErrorCode(w, r, "ALM001", "alertmanager not configured")
		return
	}

	groups, err := h.alertmanagerClient.GetAlertGroups(r.Context())
	if err != nil {
		writeErrorCode(w, r, "ALM002", "failed to fetch alert groups")
		return
	}

	writeJSON(w, groups)
}

// filterAlertsByACL removes alerts the user doesn't have read access to.
// Unlinked alerts (no service/node match) are visible to all authenticated users.
func (h *Handlers) filterAlertsByACL(r *http.Request, alerts []cache.CachedAlert) []cache.CachedAlert {
	if h.acl == nil {
		return alerts
	}
	id := identityFromRequest(r)
	result := make([]cache.CachedAlert, 0, len(alerts))
	for _, a := range alerts {
		if h.canReadAlertWithIdentity(id, a) {
			result = append(result, a)
		}
	}
	return result
}

func (h *Handlers) canReadAlert(r *http.Request, a cache.CachedAlert) bool {
	if h.acl == nil {
		return true
	}
	return h.canReadAlertWithIdentity(identityFromRequest(r), a)
}

func (h *Handlers) canReadAlertWithIdentity(id any, a cache.CachedAlert) bool {
	// Unlinked alerts are visible to all authenticated users.
	if a.ServiceID == "" && a.NodeID == "" && a.StackName == "" {
		return true
	}
	if a.ServiceID != "" && h.acl.Can(id.(*auth.Identity), "read", "service:"+a.ServiceName) {
		return true
	}
	if a.NodeID != "" && h.acl.Can(id.(*auth.Identity), "read", "node:"+a.NodeName) {
		return true
	}
	return false
}
```

Note: This is a sketch — the exact `applySortAndSearch`, `writeCollectionResponse`, `writeDetailResponse`, `writeNotFound`, and `identityFromRequest` helpers already exist in `handlers.go`. The implementer should use the same patterns as `HandleListNodes`/`HandleGetNode`. The ACL integration needs to use `auth.IdentityFromContext(r.Context())` from the existing `auth` package, not `identityFromRequest`.

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/api/`
Expected: success (may need adjustments to match exact helper signatures)

- [ ] **Step 3: Commit**

```bash
git add internal/api/alert_handlers.go
git commit -m "feat(api): add alert list, detail, and groups handlers"
```

---

## Task 8: Silence API Handlers

**Files:**
- Create: `internal/api/silence_handlers.go`

- [ ] **Step 1: Create the silence handlers**

```go
package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/alertmanager"
)

func (h *Handlers) HandleListSilences(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	if h.alertmanagerClient == nil {
		writeErrorCode(w, r, "ALM001", "alertmanager not configured")
		return
	}

	silences, err := h.alertmanagerClient.GetSilences(r.Context())
	if err != nil {
		writeErrorCode(w, r, "ALM003", "failed to fetch silences")
		return
	}

	writeJSON(w, silences)
}

func (h *Handlers) HandleCreateSilence(w http.ResponseWriter, r *http.Request) {
	if h.alertmanagerClient == nil {
		writeErrorCode(w, r, "ALM001", "alertmanager not configured")
		return
	}

	var req alertmanager.CreateSilenceRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		return
	}

	// TODO: ACL check — resolve matchers to resources, require write on matched resources.

	id, err := h.alertmanagerClient.CreateSilence(r.Context(), req)
	if err != nil {
		writeErrorCode(w, r, "ALM004", "failed to create silence")
		return
	}

	writeJSON(w, map[string]string{"id": id})
}

func (h *Handlers) HandleExpireSilence(w http.ResponseWriter, r *http.Request) {
	if h.alertmanagerClient == nil {
		writeErrorCode(w, r, "ALM001", "alertmanager not configured")
		return
	}

	silenceID := r.PathValue("id")

	// TODO: ACL check — fetch silence, resolve matchers to resources, require write.

	if err := h.alertmanagerClient.ExpireSilence(r.Context(), silenceID); err != nil {
		writeErrorCode(w, r, "ALM005", "failed to expire silence")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

Note: The TODO comments for ACL are intentional — the implementer should follow the same pattern as `requireWriteACL` in `write_middleware.go`, resolving silence matchers to resource names. The exact implementation depends on how matchers map to resources.

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/api/`
Expected: success (may need adjustment for `decodeJSONBody` — use the existing JSON decode pattern)

- [ ] **Step 3: Commit**

```bash
git add internal/api/silence_handlers.go
git commit -m "feat(api): add silence list, create, and expire handlers"
```

---

## Task 9: Wire Handlers, Router, and main.go

**Files:**
- Modify: `internal/api/handlers.go` — Add `alertmanagerClient` field
- Modify: `internal/api/router.go` — Register `/alerting/` routes + webhook
- Modify: `internal/api/cluster_handlers.go` — Extend MonitoringStatus
- Modify: `main.go` — Initialize Alertmanager client, wire webhook

- [ ] **Step 1: Add alertmanagerClient to Handlers struct**

In `internal/api/handlers.go`, add to `Handlers` struct (after `acl` field, line 233):

```go
alertmanagerClient *alertmanager.Client
```

Update `NewHandlers` to accept and store it. Add parameter after `aclEval *acl.Evaluator`:

```go
alertmanagerClient *alertmanager.Client,
```

And in the returned struct:

```go
alertmanagerClient: alertmanagerClient,
```

- [ ] **Step 2: Register alerting routes in router.go**

In `internal/api/router.go`, add after the search routes (around line 546) and before the profile route:

```go
// Alerting
mux.HandleFunc(
	"GET /alerting/alerts",
	contentNegotiatedWithSSE(
		h.HandleListAlerts,
		func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, cache.EventAlert) },
		h.listFeeds("Alerts", cache.EventAlert),
		spa,
	),
)
mux.HandleFunc(
	"GET /alerting/alerts/{fingerprint}",
	contentNegotiatedWithSSE(
		h.HandleGetAlert,
		func(w http.ResponseWriter, r *http.Request) {
			h.streamResource(w, r, cache.EventAlert, r.PathValue("fingerprint"))
		},
		h.detailFeeds(cache.EventAlert, "fingerprint", func(fp string) string {
			if a, ok := h.cache.GetAlert(fp); ok {
				return a.Labels["alertname"]
			}
			return fp
		}),
		spa,
	),
)
mux.HandleFunc(
	"GET /alerting/alerts/groups",
	contentNegotiated(h.HandleAlertGroups, feedHandlers{}, spa),
)

// Silences
mux.HandleFunc(
	"GET /alerting/silences",
	contentNegotiated(h.HandleListSilences, feedHandlers{}, spa),
)
mux.Handle("POST /alerting/silences", tier1(h.HandleCreateSilence))
mux.Handle("DELETE /alerting/silences/{id}", tier1(h.HandleExpireSilence))
```

- [ ] **Step 3: Extend MonitoringStatus**

In `internal/api/cluster_handlers.go`, add to `MonitoringStatus` struct:

```go
AlertmanagerConfigured bool   `json:"alertmanagerConfigured"`
AlertmanagerReachable  bool   `json:"alertmanagerReachable"`
AlertmanagerURL        string `json:"alertmanagerUrl,omitempty"`
```

In `HandleMonitoringStatus`, add after the Prometheus checks (after `wg.Wait()`, before `writeJSON`):

```go
if h.alertmanagerClient != nil {
	status.AlertmanagerConfigured = true
	status.AlertmanagerURL = h.alertmanagerClient.URL()
	if err := h.alertmanagerClient.GetStatus(ctx); err == nil {
		status.AlertmanagerReachable = true
	}
}
```

- [ ] **Step 4: Wire in main.go**

In `main.go`, after the Prometheus initialization block (around line 243) and before the recommendations engine:

```go
// Alertmanager
var amClient *alertmanager.Client
if cfg.AlertmanagerURL != "" {
	amClient = alertmanager.NewClient(cfg.AlertmanagerURL)
	slog.Info("alertmanager configured", "url", cfg.AlertmanagerURL)
} else if cfg.PrometheusURL != "" {
	// Auto-discover from Prometheus
	discovered, err := alertmanager.Discover(ctx, cfg.PrometheusURL)
	if err != nil {
		slog.Warn("alertmanager discovery failed", "error", err)
	} else if discovered != "" {
		amClient = alertmanager.NewClient(discovered)
		slog.Info("alertmanager discovered via prometheus", "url", discovered)
	}
}
if amClient == nil {
	slog.Info("alertmanager not configured, alerting disabled")
}
```

Update the `NewHandlers` call to pass `amClient` as the last argument.

After the watcher startup, add the initial alert sync:

```go
if amClient != nil {
	go func() {
		<-watcher.Ready()
		alerts, err := amClient.GetAlerts(ctx)
		if err != nil {
			slog.Warn("initial alert sync failed", "error", err)
			return
		}
		for _, a := range alerts {
			stateCache.SetAlert(cache.CachedAlert{
				Fingerprint:  a.Fingerprint,
				Labels:       a.Labels,
				Annotations:  a.Annotations,
				StartsAt:     a.StartsAt,
				EndsAt:       a.EndsAt,
				UpdatedAt:    a.UpdatedAt,
				GeneratorURL: a.GeneratorURL,
				State:        a.Status.State,
				SilencedBy:   a.Status.SilencedBy,
				InhibitedBy:  a.Status.InhibitedBy,
			})
		}
		slog.Info("initial alert sync complete", "count", len(alerts))
	}()
}
```

Register the webhook handler in the router (in `main.go` or `router.go`). Since webhooks go under `/-/` (exempt from auth), add to `router.go` in the meta endpoints section:

```go
if cfg.AlertmanagerWebhook != nil {
	mux.Handle("POST /-/webhooks/alertmanager", cfg.AlertmanagerWebhook)
}
```

Add `AlertmanagerWebhook http.Handler` to `RouterConfig` struct.

In `main.go`, create the webhook handler and pass it to `RouterConfig`:

```go
var alertmanagerWebhook http.Handler
if amClient != nil {
	alertmanagerWebhook = alertmanager.NewWebhookHandler(
		cfg.AlertmanagerWebhookSecret,
		func(alerts []alertmanager.WebhookAlert, status string) {
			for _, a := range alerts {
				if a.Status == "resolved" {
					stateCache.DeleteAlert(a.Fingerprint)
				} else {
					stateCache.SetAlert(cache.CachedAlert{
						Fingerprint:  a.Fingerprint,
						Labels:       a.Labels,
						Annotations:  a.Annotations,
						StartsAt:     a.StartsAt,
						EndsAt:       a.EndsAt,
						State:        a.Status,
						GeneratorURL: a.GeneratorURL,
					})
				}
			}
		},
	)
}
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build .`
Expected: success

- [ ] **Step 6: Run all Go tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./...`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers.go internal/api/router.go internal/api/cluster_handlers.go main.go
git commit -m "feat: wire alertmanager client, webhook, and routes into application"
```

---

## Task 10: Frontend Types and API Client

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add Alert and Silence types**

In `frontend/src/api/types.ts`, add:

```typescript
export interface CachedAlert {
  fingerprint: string;
  labels: Record<string, string>;
  annotations: Record<string, string>;
  startsAt: string;
  endsAt: string;
  updatedAt: string;
  generatorURL: string;
  state: string;
  silencedBy?: string[];
  inhibitedBy?: string[];
  serviceId?: string;
  serviceName?: string;
  nodeId?: string;
  nodeName?: string;
  stackName?: string;
}

export interface Silence {
  id: string;
  matchers: SilenceMatcher[];
  startsAt: string;
  endsAt: string;
  createdBy: string;
  comment: string;
  status: { state: string };
  updatedAt: string;
}

export interface SilenceMatcher {
  name: string;
  value: string;
  isRegex: boolean;
  isEqual?: boolean;
}

export interface AlertGroup {
  labels: Record<string, string>;
  receiver: { name: string };
  alerts: CachedAlert[];
}

export interface CreateSilenceRequest {
  matchers: SilenceMatcher[];
  startsAt: string;
  endsAt: string;
  createdBy: string;
  comment: string;
}
```

Extend `MonitoringStatus`:

```typescript
// Add to existing MonitoringStatus interface:
alertmanagerConfigured: boolean;
alertmanagerReachable: boolean;
alertmanagerUrl?: string;
```

Add `"alerts"` to `SearchResourceType` union.

- [ ] **Step 2: Add API client methods**

In `frontend/src/api/client.ts`, add to the `api` object:

```typescript
alerts: (params: ListParams, signal?: AbortSignal) =>
  get<CollectionResponse<CachedAlert>>(
    buildPath("/alerting/alerts", params),
    signal,
  ),

alert: (fingerprint: string, signal?: AbortSignal) =>
  get<CachedAlert>(`/alerting/alerts/${fingerprint}`, signal),

alertGroups: (signal?: AbortSignal) =>
  get<AlertGroup[]>("/alerting/alerts/groups", signal),

silences: (signal?: AbortSignal) =>
  get<Silence[]>("/alerting/silences", signal),

createSilence: (body: CreateSilenceRequest) =>
  post<{ id: string }>("/alerting/silences", body),

expireSilence: (id: string) =>
  del(`/alerting/silences/${id}`),
```

- [ ] **Step 3: Update search constants**

In `frontend/src/lib/searchConstants.ts`:

Add `"alerts"` to `typeOrder` (after `"tasks"`).
Add `alerts: "Alerts"` to `typeLabels`.
Add `case "alerts": return `/alerting/alerts/${id}`;` to `resourcePath`.

- [ ] **Step 4: Update SSE path map**

In `frontend/src/hooks/useSwarmQuery.ts`, add to `ssePathMap`:

```typescript
alert: "/alerting/alerts",
```

- [ ] **Step 5: Verify frontend builds**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts frontend/src/lib/searchConstants.ts frontend/src/hooks/useSwarmQuery.ts
git commit -m "feat(frontend): add alert/silence types, API client, and search integration"
```

---

## Task 11: Alert List Page

**Files:**
- Create: `frontend/src/pages/AlertList.tsx`

- [ ] **Step 1: Create the alert list page**

Follow the exact pattern from `NodeList.tsx`, using `useListPage` with `sseType: "alert"` and `path: "/alerting/alerts"`. Columns: alert name (from `labels.alertname`), state, severity (from `labels.severity`), affected resource (linked), started at, duration. The implementer should read `NodeList.tsx` and replicate the structure with alert-specific columns.

Key hook usage:

```typescript
const {
  data: alerts,
  loading,
  error,
  retry,
  hasMore,
  loadMore,
  search,
  setSearch,
  sortKey,
  sortDir,
  toggle,
  viewMode,
  setViewMode,
} = useListPage<CachedAlert>({
  path: "/alerting/alerts",
  sseType: "alert",
  defaultSort: "startsAt",
  viewModeKey: "alerts",
  fetchFn: (params, signal) => api.alerts(params, signal),
  keyFn: ({ fingerprint }) => fingerprint,
});
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/AlertList.tsx
git commit -m "feat(frontend): add alert list page"
```

---

## Task 12: Alert Detail Page

**Files:**
- Create: `frontend/src/pages/AlertDetail.tsx`

- [ ] **Step 1: Create the alert detail page**

Follow the `NodeDetail.tsx` pattern. Use `useDetailResource` with `api.alert` and SSE path `/alerting/alerts/{fingerprint}`. Show: labels, annotations, generatorURL link, state, cross-referenced resource links (service → `/services/{id}`, node → `/nodes/{id}`, stack → `/stacks/{name}`), activity history.

Key hook usage:

```typescript
const { data: alert, history, error } = useDetailResource<CachedAlert>(
  fingerprint,
  api.alert,
  `/alerting/alerts/${fingerprint}`,
);
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/AlertDetail.tsx
git commit -m "feat(frontend): add alert detail page"
```

---

## Task 13: Silence List Page and Form

**Files:**
- Create: `frontend/src/pages/SilenceList.tsx`
- Create: `frontend/src/components/SilenceForm.tsx`

- [ ] **Step 1: Create the silence list page**

Simple page using `useQuery` (not `useSwarmResource` — silences aren't cached/streamed). Fetches via `api.silences()`. Shows a table of silences with: matchers, state, author, comment, time range. Manual refresh button. "Create Silence" button opens the form dialog.

- [ ] **Step 2: Create the silence form dialog**

Dialog component with fields: matchers (key/value pairs, add/remove), start time, end time (or duration), comment. Pre-fill matchers when opened from alert context. Submits via `api.createSilence()`.

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/SilenceList.tsx frontend/src/components/SilenceForm.tsx
git commit -m "feat(frontend): add silence list page and creation form"
```

---

## Task 14: Routing, Navigation, and Alert Badge

**Files:**
- Modify: `frontend/src/App.tsx`
- Create: `frontend/src/components/AlertBadge.tsx`

- [ ] **Step 1: Add lazy imports**

In `App.tsx`, add with the other lazy imports:

```typescript
const AlertList = lazy(() => import("./pages/AlertList"));
const AlertDetail = lazy(() => import("./pages/AlertDetail"));
const SilenceList = lazy(() => import("./pages/SilenceList"));
```

- [ ] **Step 2: Add routes**

In `App.tsx`, add within `<Routes>`:

```typescript
<Route path="/alerting/alerts" element={<AlertList />} />
<Route path="/alerting/alerts/:fingerprint" element={<AlertDetail />} />
<Route path="/alerting/silences" element={<SilenceList />} />
```

- [ ] **Step 3: Add nav link**

In the `NavLinks` component `links` array, add:

```typescript
{ to: "/alerting/alerts", label: "Alerts", keys: ["g", "l"] },
```

And the corresponding hotkey in `useHotkeys`:

```typescript
"g l": useCallback(() => navigate("/alerting/alerts"), [navigate]),
```

- [ ] **Step 4: Create AlertBadge component**

A small component that shows the firing alert count in the nav bar. Uses a lightweight query or SSE subscription to `AlertCount`. Renders a small red badge next to the "Alerts" nav link when count > 0.

- [ ] **Step 5: Verify frontend builds**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add frontend/src/App.tsx frontend/src/components/AlertBadge.tsx
git commit -m "feat(frontend): add alerting routes, nav link, and alert count badge"
```

---

## Task 15: Monitoring Status Extension

**Files:**
- Modify: `frontend/src/hooks/useMonitoringStatus.ts`
- Modify: `frontend/src/components/metrics/MonitoringStatus.tsx`

- [ ] **Step 1: Add isAlertmanagerReady helper**

In `frontend/src/hooks/useMonitoringStatus.ts`, add:

```typescript
export function isAlertmanagerReady(status: MonitoringStatus | null): boolean {
  return !!status?.alertmanagerConfigured && status?.alertmanagerReachable;
}
```

- [ ] **Step 2: Extend MonitoringStatus component**

In the `MonitoringStatus` component, add an Alertmanager tier following the same pattern as Prometheus/cAdvisor/node-exporter. Show: unconfigured (dismissible), unreachable (warning), healthy (hidden).

- [ ] **Step 3: Verify frontend builds**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/hooks/useMonitoringStatus.ts frontend/src/components/metrics/MonitoringStatus.tsx
git commit -m "feat(frontend): add alertmanager tier to monitoring status"
```

---

## Task 16: Alert Badges on Existing Pages

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`
- Modify: `frontend/src/pages/NodeDetail.tsx`

- [ ] **Step 1: Add alert count to service detail**

In `ServiceDetail.tsx`, fetch alerts for the service from the detail response's `alerts` field (added by the backend cross-reference extension). Show a badge next to the page header when count > 0, linking to `/alerting/alerts?filter=serviceId=="..."`.

- [ ] **Step 2: Add alert count to node detail**

Same pattern for `NodeDetail.tsx`.

- [ ] **Step 3: Verify frontend builds**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx frontend/src/pages/NodeDetail.tsx
git commit -m "feat(frontend): add alert badges on service and node detail pages"
```

---

## Task 17: Predefined Prometheus Rules File

**Files:**
- Create: `rules/cetacean.rules.yml`

- [ ] **Step 1: Create the rules file**

The implementer should audit the existing PromQL queries in `frontend/src/hooks/useServiceMetrics.ts`, `frontend/src/hooks/useNodeMetrics.ts`, and `internal/recommendations/` to extract the exact queries. The rules file structure:

```yaml
groups:
  - name: cetacean_recording_rules
    interval: 30s
    rules:
      - record: cetacean:service_cpu:rate5m
        expr: |
          sum by (container_label_com_docker_swarm_service_name) (
            rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name!=""}[5m])
          )
      - record: cetacean:service_memory:bytes
        expr: |
          sum by (container_label_com_docker_swarm_service_name) (
            container_memory_usage_bytes{container_label_com_docker_swarm_service_name!=""}
          )
      - record: cetacean:node_cpu:percent
        expr: |
          100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)
      - record: cetacean:node_memory:percent
        expr: |
          (1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100
      - record: cetacean:node_disk:percent
        expr: |
          100 - (node_filesystem_avail_bytes{mountpoint="/",fstype!="tmpfs"} / node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs"} * 100)

  - name: cetacean_alerting_rules
    rules:
      - alert: ServiceDown
        expr: |
          count by (container_label_com_docker_swarm_service_name) (
            container_last_seen{container_label_com_docker_swarm_service_name!=""}
          ) == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Service {{ $labels.container_label_com_docker_swarm_service_name }} has no running tasks"

      - alert: ServiceRestartLoop
        expr: |
          increase(container_restart_count{container_label_com_docker_swarm_service_name!=""}[15m]) > 5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Service {{ $labels.container_label_com_docker_swarm_service_name }} is restarting frequently"

      - alert: NodeDiskPressure
        expr: cetacean:node_disk:percent > 85
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Node {{ $labels.instance }} disk usage is above 85%"

      - alert: NodeMemoryPressure
        expr: cetacean:node_memory:percent > 90
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Node {{ $labels.instance }} memory usage is above 90%"

      - alert: NodeDown
        expr: up{job="node-exporter"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Node {{ $labels.instance }} is unreachable"

      - alert: ServiceDegraded
        expr: |
          count by (container_label_com_docker_swarm_service_name) (
            container_last_seen{container_label_com_docker_swarm_service_name!=""}
          ) < on(container_label_com_docker_swarm_service_name) group_left()
          (count by (container_label_com_docker_swarm_service_name) (
            container_last_seen{container_label_com_docker_swarm_service_name!=""}
          ))
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Service {{ $labels.container_label_com_docker_swarm_service_name }} has fewer running tasks than desired"

      - alert: NodeDrained
        expr: |
          node_meta_availability{availability="drain"} == 1
        labels:
          severity: info
        annotations:
          summary: "Node {{ $labels.instance }} availability is set to drain"
```

Note: The exact PromQL should be finalized by auditing the frontend hooks and the recommendations engine. ServiceDegraded needs refinement — Swarm desired replica counts aren't directly in Prometheus, so the implementer may need to use a different approach (e.g., a recording rule from Cetacean's cache, or comparing container counts against a label). NodeDrained requires a custom metric or relying on Cetacean's own alerting rules rather than node-exporter. The above is a starting point.

- [ ] **Step 2: Commit**

```bash
git add rules/cetacean.rules.yml
git commit -m "feat: add predefined Prometheus recording and alerting rules"
```

---

## Task 18: Cross-reference Alerts in Existing Detail Endpoints

**Files:**
- Modify: `internal/api/handlers.go`

The spec requires that service, node, and stack detail responses gain an `alerts` field listing firing/pending alerts for that resource. This follows the same pattern as `services` appearing in config/secret/network detail responses.

- [ ] **Step 1: Add alerts to service detail response**

In `HandleGetService`, after building the detail response, add:

```go
alerts := h.cache.AlertsForService(svc.ID)
```

Include `alerts` in the extras map passed to `writeDetailResponse`.

- [ ] **Step 2: Add alerts to node detail response**

Same pattern in `HandleGetNode` using `h.cache.AlertsForNode(node.ID)`.

- [ ] **Step 3: Add alerts to stack detail response**

Same pattern in `HandleGetStack` using `h.cache.AlertsForStack(stack.Name)`.

- [ ] **Step 4: Run Go tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers.go
git commit -m "feat(api): add cross-referenced alerts to service/node/stack detail responses"
```

---

## Task 19: Search Integration (Backend)


**Files:**
- Modify: `internal/api/search_handlers.go`

- [ ] **Step 1: Include alerts in global search**

In `HandleSearch`, add alerts as a new search type. Follow the same parallel search pattern — search alert names and labels against the query string. Return results with `type: "alerts"`, `id: fingerprint`, `name: alertname`, `state: alert.State`.

- [ ] **Step 2: Run all Go tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./...`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/search_handlers.go
git commit -m "feat(api): include alerts in global search results"
```

---

## Task 20: Update Monitoring Compose File

**Files:**
- Modify: `compose.monitoring.yaml`

- [ ] **Step 1: Add Alertmanager service**

Add an Alertmanager service to the monitoring stack:

```yaml
alertmanager:
  image: prom/alertmanager:v0.28.1
  networks:
    - monitoring
  deploy:
    placement:
      constraints:
        - node.role == manager
    resources:
      limits:
        memory: 256M
  healthcheck:
    test: ["CMD", "wget", "--spider", "-q", "http://localhost:9093/-/healthy"]
    interval: 15s
    timeout: 5s
    retries: 3
```

- [ ] **Step 2: Commit**

```bash
git add compose.monitoring.yaml
git commit -m "feat: add Alertmanager to monitoring stack"
```

---

## Task 21: Documentation

**Files:**
- Modify: `CLAUDE.md` — Add `CETACEAN_ALERTMANAGER_URL`, `CETACEAN_ALERTMANAGER_WEBHOOK_SECRET` to env var table; add `/alerting/` routes to key conventions; update architecture sections
- Modify: `docs/configuration.md` — Add alertmanager config section
- Modify: `docs/monitoring.md` — Add Alertmanager setup instructions, predefined rules file
- Modify: `docs/api.md` — Add alerting endpoints
- Modify: `CHANGELOG.md` — Add entry under `[Unreleased]`

- [ ] **Step 1: Update CLAUDE.md**

Add the new env vars, routes, and architecture notes.

- [ ] **Step 2: Update user docs**

Add alertmanager setup, configuration, and API reference to the relevant docs.

- [ ] **Step 3: Update CHANGELOG.md**

Add under `[Unreleased]`:

```markdown
### Added
- Alertmanager integration: view alerts, manage silences, cross-reference alerts to services/nodes/stacks
- Real-time alert updates via Alertmanager webhooks and SSE streaming
- Predefined Prometheus recording and alerting rules for Swarm clusters (`rules/cetacean.rules.yml`)
- Alert badges on service and node detail pages
- Global search includes alerts
- Alertmanager auto-discovery via Prometheus
```

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/configuration.md docs/monitoring.md docs/api.md CHANGELOG.md
git commit -m "docs: add Alertmanager integration documentation"
```

---

## Task 22: Final Verification

- [ ] **Step 1: Run full backend test suite**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./...`
Expected: all PASS

- [ ] **Step 2: Run frontend type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Run frontend lint**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`
Expected: no errors

- [ ] **Step 4: Full build**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run build && cd .. && go build -o cetacean .`
Expected: success
