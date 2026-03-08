# Production Hardening Design

Date: 2026-03-08

## Overview

Improvements informed by patterns from Portainer, Dozzle, Traefik, and Grafana. Focuses on operational basics every production Go service needs, plus targeted feature additions that leverage existing infrastructure.

## Scope

| Item | Category | Effort |
|------|----------|--------|
| Health and readiness endpoints | Operations | Small |
| Structured logging with `log/slog` | Operations | Medium |
| Request logging middleware | Operations | Small |
| Recovery middleware | Operations | Small |
| Cluster resource totals | Feature | Small |
| Graceful degradation in full sync | Resilience | Small |
| Cache snapshot-to-disk for fast restart | Resilience | Medium |
| Notification webhooks on event patterns | Feature | Medium |

### Out of scope

- gRPC agent architecture (Phase 4 / horizontal scaling concern)
- Container exec/attach (changes security model, requires separate design)
- Router migration to chi (not justified yet — stdlib works fine with few middleware)
- Label-based filtering (nice-to-have, not hardening)

---

## 1. Health and Readiness Endpoints

**Problem:** No way for load balancers, Swarm health checks, or monitoring systems to know if Cetacean is alive and ready to serve.

**Design:**

Two endpoints, both on the existing API router:

`GET /api/health` — Always returns 200 if the HTTP server is listening. No dependencies checked. Used for liveness probes.

```json
{"status": "ok"}
```

`GET /api/ready` — Returns 200 only after the initial Docker sync completes. Returns 503 before that. Used for readiness probes and load balancer registration.

```json
{"status": "ready", "uptime": "2h15m"}
```

**Implementation:** The watcher already has a `Ready()` channel. Pass a readiness flag (or the channel itself) to the handlers. The health handler is unconditional; the readiness handler checks the flag.

**Docker Compose integration:**
```yaml
healthcheck:
  test: ["CMD", "wget", "-qO-", "http://localhost:9000/api/ready"]
  interval: 10s
  timeout: 3s
  retries: 3
  start_period: 30s
```

---

## 2. Structured Logging with `log/slog`

**Problem:** The app uses `log.Printf` everywhere — no levels, no structured fields, no JSON output. Makes production debugging and log aggregation difficult.

**Design:**

Use Go's stdlib `log/slog` (available since Go 1.21). No external dependency needed.

**Logger setup in `main.go`:**
```go
handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
})
logger := slog.New(handler)
slog.SetDefault(logger)
```

Configurable via environment variable:
```
CETACEAN_LOG_LEVEL=debug  # debug, info, warn, error
CETACEAN_LOG_FORMAT=json  # json, text
```

Default: `info` level, `json` format (for production). `text` format uses the human-readable slog text handler (better for development).

**Migration:** Replace all `log.Printf` / `log.Println` calls with `slog.Info`, `slog.Warn`, `slog.Error`, adding structured fields:

```go
// Before:
log.Printf("full sync complete: %d nodes, %d services", snap.NodeCount, snap.ServiceCount)

// After:
slog.Info("full sync complete",
    "nodes", snap.NodeCount,
    "services", snap.ServiceCount,
    "tasks", snap.TaskCount,
    "stacks", snap.StackCount,
)
```

**Where to add context:**
- Watcher: sync status, event counts, reconnection attempts
- Handlers: not needed (request logging middleware covers this)
- SSE: client connect/disconnect, batch sizes
- Cache: not needed (operations are logged via watcher/handlers)

---

## 3. Request Logging Middleware

**Problem:** No visibility into API request patterns, latencies, or errors.

**Design:**

A middleware that logs every request with structured fields. Wraps the router in `main.go`.

```go
func requestLogger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        sw := &statusWriter{ResponseWriter: w, status: 200}
        next.ServeHTTP(sw, r)
        duration := time.Since(start)

        // Skip SSE and log streams (long-lived connections logged on close instead)
        if sw.status == 200 && isStreamingRequest(r) {
            return
        }

        slog.Info("request",
            "method", r.Method,
            "path", r.URL.Path,
            "status", sw.status,
            "duration_ms", duration.Milliseconds(),
            "size", sw.written,
        )
    })
}
```

`statusWriter` wraps `http.ResponseWriter` to capture the status code and bytes written.

**Skip streaming requests:** SSE (`/api/events`) and log streams (`/api/*/logs` with `Accept: text/event-stream`) are long-lived. Log their start, not every tick. These get a single log line on connection close.

**Placement:** Wraps the entire mux in the middleware chain: `securityHeaders(requestLogger(mux))`.

---

## 4. Recovery Middleware

**Problem:** A panic in any handler crashes the entire process.

**Design:**

```go
func recovery(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if err := recover(); err != nil {
                slog.Error("panic recovered",
                    "error", err,
                    "path", r.URL.Path,
                    "stack", string(debug.Stack()),
                )
                http.Error(w, "internal server error", http.StatusInternalServerError)
            }
        }()
        next.ServeHTTP(w, r)
    })
}
```

**Placement:** Outermost middleware: `recovery(securityHeaders(requestLogger(mux)))`.

---

## 5. Cluster Resource Totals

**Problem:** The cluster overview shows node/service/task counts but not total CPU and memory capacity. Portainer computes this from node descriptions during snapshots.

**Design:**

Extend `ClusterSnapshot` in `cache.go`:

```go
type ClusterSnapshot struct {
    // ... existing fields ...
    TotalCPU    int   `json:"totalCPU"`    // total CPU cores across all nodes
    TotalMemory int64 `json:"totalMemory"` // total memory bytes across all nodes
}
```

Computed in `Snapshot()` by summing `node.Description.Resources.NanoCPUs` (converted to cores) and `node.Description.Resources.MemoryBytes` across all nodes.

**Frontend:** Add two stat cards to ClusterOverview showing total CPU cores and total memory (formatted as GB).

---

## 6. Graceful Degradation in Full Sync

**Problem:** If one of the 7 parallel sync goroutines fails (e.g., ListConfigs returns an error), that resource type gets `nil` in `FullSyncData`. The `ReplaceAll` call then replaces the existing data with an empty map, losing previously known state.

**Current behavior (watcher.go:204-209):**
```go
for i := 0; i < 7; i++ {
    r := <-ch
    if r.err != nil {
        log.Printf("full sync %s failed: %v", r.name, r.err)
    }
}
w.store.ReplaceAll(data) // replaces even if some fields are nil
```

**Design:**

Add a `Valid` bitmap (or bool per field) to `FullSyncData` so `ReplaceAll` only replaces resource types that were successfully fetched:

```go
type FullSyncData struct {
    Nodes    []swarm.Node
    Services []swarm.Service
    Tasks    []swarm.Task
    Configs  []swarm.Config
    Secrets  []swarm.Secret
    Networks []network.Summary
    Volumes  []volume.Volume

    // Which fields were successfully fetched (nil = skip replacement)
    HasNodes, HasServices, HasTasks    bool
    HasConfigs, HasSecrets             bool
    HasNetworks, HasVolumes            bool
}
```

In the watcher, set the corresponding `Has*` flag when the fetch succeeds. In `ReplaceAll`, only replace maps where the flag is true.

This matches Portainer's pattern where each sub-snapshot logs a warning but doesn't block the rest.

---

## 7. Cache Snapshot-to-Disk for Fast Restart

**Problem:** On restart, Cetacean must do a full Docker API sync before serving any requests. For large clusters, this takes several seconds. The in-memory cache and event journal are lost entirely.

**Design:**

Periodically serialize the cache to a JSON file on disk. On startup, load from the snapshot file to pre-populate the cache, then immediately start serving (possibly stale data) while the full sync runs in the background.

**Snapshot file:** `CETACEAN_DATA_DIR/snapshot.json` (default: `/var/lib/cetacean/snapshot.json` or `./data/snapshot.json`).

**Contents:**
```json
{
  "version": 1,
  "timestamp": "2026-03-08T12:00:00Z",
  "nodes": [...],
  "services": [...],
  "tasks": [...],
  "configs": [...],
  "secrets": [...],
  "networks": [...],
  "volumes": [...]
}
```

**Write schedule:** Every 5 minutes (aligned with the existing periodic re-sync). Write to a temp file then rename for atomicity.

**Startup flow:**
1. Load snapshot from disk (if exists and version matches)
2. Populate cache from snapshot
3. Mark server as ready (serve potentially stale data)
4. Start full sync in background
5. Full sync replaces snapshot data with fresh data
6. Log staleness: `"loaded snapshot from disk, age=2m15s, starting fresh sync"`

**Staleness indicator:** The frontend could show a "data may be stale" banner if the snapshot is older than 10 minutes, dismissed once the first live sync completes. This requires a new field in `GET /api/cluster`:

```json
{
  "snapshotAge": 135,
  "syncing": true
}
```

**Config:**
```
CETACEAN_DATA_DIR=/var/lib/cetacean   # directory for snapshot file
CETACEAN_SNAPSHOT=true                 # enable/disable (default: true)
```

**Not persisted:** The event journal (history ring buffer) is NOT written to the snapshot. It's ephemeral by design — the snapshot is about fast restart, not audit history.

---

## 8. Notification Webhooks on Event Patterns

**Problem:** You must be looking at the dashboard to notice something went wrong. No way to get alerted when a service fails, a node goes down, or tasks restart repeatedly.

**Design (inspired by Dozzle's notification rules engine):**

A lightweight, built-in notification system that watches the event journal and fires webhooks when patterns match.

### Rule model

```go
type NotificationRule struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    Enabled   bool   `json:"enabled"`
    Match     Match  `json:"match"`
    Webhook   string `json:"webhook"`
    Cooldown  string `json:"cooldown"` // e.g., "5m" — suppress duplicate alerts
}

type Match struct {
    Type       string `json:"type,omitempty"`       // "node", "service", "task"
    Action     string `json:"action,omitempty"`      // "update", "remove"
    NameRegex  string `json:"nameRegex,omitempty"`   // regex on resource name
    Condition  string `json:"condition,omitempty"`    // "task.state == failed", "node.state == down"
}
```

### Example rules

```json
[
  {
    "id": "node-down",
    "name": "Node went down",
    "enabled": true,
    "match": {"type": "node", "condition": "state == down"},
    "webhook": "https://hooks.slack.com/...",
    "cooldown": "5m"
  },
  {
    "id": "task-failed",
    "name": "Task failed",
    "enabled": true,
    "match": {"type": "task", "condition": "state == failed"},
    "webhook": "https://hooks.slack.com/...",
    "cooldown": "1m"
  }
]
```

### Configuration

Rules are defined in a YAML/JSON config file, NOT in the UI. Cetacean is read-only — configuration belongs in files, not in a database.

```
CETACEAN_NOTIFICATIONS_FILE=/etc/cetacean/notifications.json
```

### Webhook payload

```json
{
  "rule": "task-failed",
  "timestamp": "2026-03-08T12:00:00Z",
  "event": {
    "type": "task",
    "action": "update",
    "resourceId": "abc123",
    "name": "nginx.3"
  },
  "message": "Task nginx.3 entered state: failed"
}
```

### Architecture

- A `Notifier` goroutine subscribes to the cache's `OnChangeFunc` (same as the SSE broadcaster)
- On each event, it evaluates all enabled rules
- On match, it checks cooldown (last fire time per rule)
- If not cooled down, it fires an async HTTP POST to the webhook URL
- Fire-and-forget with a 5-second timeout and at most 1 retry
- Cooldown state is in-memory only (resets on restart)

### Condition evaluation

Keep it simple — no expression parser. Support a fixed set of conditions:

| Condition | Evaluates |
|-----------|-----------|
| `state == <value>` | Task state or node state matches value |
| `action == <value>` | Event action matches (update, remove) |

If `condition` is empty, the rule matches on `type` and `action` alone. `nameRegex` adds an optional name filter.

### API endpoints (read-only, consistent with Cetacean's philosophy)

`GET /api/notifications/rules` — List all configured rules and their cooldown state (last fired timestamp).

No create/update/delete endpoints — rules are file-managed.

---

## Middleware Chain Summary

After all changes, the middleware chain in `main.go`:

```
recovery → securityHeaders → requestLogger → mux
```

Applied as:
```go
router := api.NewRouter(handlers, broadcaster, promProxy, spa)
handler := recovery(securityHeaders(requestLogger(router)))
```

---

## Configuration Summary

New environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `CETACEAN_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `CETACEAN_LOG_FORMAT` | `json` | Log format: json, text |
| `CETACEAN_DATA_DIR` | `./data` | Directory for snapshot file |
| `CETACEAN_SNAPSHOT` | `true` | Enable cache snapshot-to-disk |
| `CETACEAN_NOTIFICATIONS_FILE` | — | Path to notification rules JSON (optional) |
