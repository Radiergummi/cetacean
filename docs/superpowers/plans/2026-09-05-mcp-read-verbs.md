# MCP Read Verbs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the four missing read verbs — a landing status call, a filterable change timeline, cluster-wide log scopes, and a server-side wait — so the questions that currently need enumerate-then-fan-out become one call each.

**Architecture:** Every new verb is a tool in `internal/mcp` over a rule that lives in `internal/cluster`, following the split `find`/`describe` already use: the transport handles arguments and ACL, the domain package holds the shape and the derivation, so REST and MCP cannot drift. Two verbs reuse machinery that already exists but is unreachable — `get_events` exposes `cache.HistoryQuery`, whose filters the Atom feeds already use and the MCP resource discards; `watch` exposes `cluster.ServiceConverged`, reachable today only as a side effect of a mutation.

**Tech Stack:** Go 1.26, `mark3labs/mcp-go` v1.0.0, Docker Engine API types.

**Spec:** `docs/superpowers/specs/2026-09-04-mcp-agent-surface-design.md` (this is plan 2 of 3; plan 1 shipped as `b46730c8` and its predecessors, plan 3 is `2026-09-05-mcp-write-surface.md`)

## Global Constraints

- **A tool is the unit of visibility.** `tools/list` is filtered by operations tier *and* by `toolVisibilityFor`'s per-type ACL check. If a caller can see a tool, every part of it must be callable. Collapse only within one tier **and** one target resource type. Never compute a tier at call time.
- **Units are named, never implied.** Every numeric field carries its unit in its name (`memoryLimitBytes`, `cpuLimitCores`) or is a duration string (`"10s"`).
- **Env variable names only, never values.** Env carries credentials.
- **Secret data stays zeroed.** Reads go through `lookupResource`, which already redacts; do not bypass it.
- **Nil slices must not marshal to `null`.** Every slice in a type with an advertised output schema is initialised.
- **Sorted output.** Results are marshalled into MCP results a client may cache by ETag; range-over-map ordering must be sorted before returning.
- **Every tool declares `WithOutputSchema`.** `TestEveryToolAdvertisesOutputSchema` walks the whole registry and fails a tool that does not.
- All new exported types and functions carry doc comments in the existing house style: say *why*, not *what*.

## File Structure

| File | Responsibility |
|---|---|
| `internal/cluster/timeline.go` (create) | `TimelineEntry` — the one shape `get_events` and `get_logs` both return, so intent 29 is one interleaved read rather than a correlation the model performs. |
| `internal/cluster/status.go` (create) | `ClusterStatus` and `BuildClusterStatus` — the landing shape, naming unhealthy resources rather than counting them. |
| `internal/mcp/events.go` (create) | The `get_events` tool: argument parsing, ACL filtering, `cache.HistoryQuery` construction. |
| `internal/mcp/status.go` (create) | The `get_cluster_status` tool: gathers ACL-filtered slices and calls `cluster.BuildClusterStatus`. |
| `internal/mcp/logs_scope.go` (create) | Stack and cluster log scopes: fan-out, per-service cap, `contains` filter, combined cursor. |
| `internal/mcp/watch.go` (create) | The `watch` tool, over the existing `awaitConvergence`. |
| `internal/mcp/tools.go` (modify) | Register the four tools in `toolCatalog()`; extend `get_logs`' schema. |
| `internal/mcp/server.go` (modify) | `toolACLSpecs` entries where a tool is type-gated. |
| `docs/mcp.md`, `CHANGELOG.md` (modify) | Documentation. |

---

### Task 1: The timeline shape

**Files:**
- Create: `internal/cluster/timeline.go`
- Test: `internal/cluster/timeline_test.go`

**Interfaces:**
- Produces: `cluster.TimelineEntry` struct; `cluster.SortTimeline(entries []TimelineEntry)`.

Intent 29 — "this started at 14:02, what else happened?" — is the highest-value question in incident response and today requires the model to read logs, read history, and correlate two different shapes by eye. One shape makes it one read.

- [ ] **Step 1: Write the failing test**

```go
package cluster

import "testing"

// A timeline is read newest-first, and a log line and a change event that
// happened in the same second must be orderable against each other — that is
// the whole reason the two share a shape.
func TestSortTimelineOrdersNewestFirst(t *testing.T) {
	entries := []TimelineEntry{
		{At: "2026-09-05T14:00:00Z", Kind: "change", Message: "older"},
		{At: "2026-09-05T14:02:00Z", Kind: "log", Message: "newer"},
		{At: "2026-09-05T14:01:00Z", Kind: "change", Message: "middle"},
	}

	SortTimeline(entries)

	want := []string{"newer", "middle", "older"}
	for i, w := range want {
		if entries[i].Message != w {
			t.Errorf("entry %d = %q, want %q", i, entries[i].Message, w)
		}
	}
}

// Ties break on Kind then Message so the order is total: an ETag over the
// result must not change between two calls that saw the same data.
func TestSortTimelineIsDeterministicOnTies(t *testing.T) {
	a := []TimelineEntry{
		{At: "2026-09-05T14:00:00Z", Kind: "log", Message: "b"},
		{At: "2026-09-05T14:00:00Z", Kind: "change", Message: "a"},
	}
	b := []TimelineEntry{
		{At: "2026-09-05T14:00:00Z", Kind: "change", Message: "a"},
		{At: "2026-09-05T14:00:00Z", Kind: "log", Message: "b"},
	}

	SortTimeline(a)
	SortTimeline(b)

	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("entry %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cluster/ -run TestSortTimeline`
Expected: FAIL — `undefined: TimelineEntry`, `undefined: SortTimeline`.

- [ ] **Step 3: Write the type and the sort**

```go
package cluster

import (
	"slices"
	"strings"
)

// TimelineEntry is one thing that happened, whether Cetacean observed it as a
// resource change or a container wrote it to stdout.
//
// The two share a shape so that "this started at 14:02 — what else happened?"
// is a single ordered read rather than a correlation the caller performs
// across two payloads with different field names and different time formats.
// That question is the one incident response actually turns on, and it was
// unanswerable while history and logs were separate shapes.
type TimelineEntry struct {
	// At is RFC 3339, always UTC, at fixed nanosecond width so string
	// comparison is time comparison and a cursor can be a plain string.
	At string `json:"at"`

	// Kind is "change" or "log".
	Kind string `json:"kind"`

	// Type is the resource type for a change ("service", "task", ...), and
	// the stream for a log line ("stdout", "stderr").
	Type string `json:"type,omitempty"`

	// Name is the resource the entry concerns, named the way a reader would
	// say it: a service's name, a task's "<service>.<slot>".
	Name string `json:"name,omitempty"`

	// ResourceID addresses the resource, for a follow-up describe.
	ResourceID string `json:"resourceId,omitempty"`

	// Message is the change's action ("create", "update", "delete") or the
	// log line's text.
	Message string `json:"message,omitempty"`
}

// SortTimeline orders entries newest first, breaking ties on kind and message.
//
// The tie-break exists for determinism rather than meaning: a result may be
// cached by ETag, and two calls that saw identical data must serialise
// identically. Map iteration and merge order do not guarantee that on their
// own.
func SortTimeline(entries []TimelineEntry) {
	slices.SortStableFunc(entries, func(a, b TimelineEntry) int {
		if c := strings.Compare(b.At, a.At); c != 0 {
			return c
		}
		if c := strings.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}

		return strings.Compare(a.Message, b.Message)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cluster/ -run TestSortTimeline`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cluster/timeline.go internal/cluster/timeline_test.go
git commit -m "feat(cluster): add the shared timeline shape for events and logs"
```

---

### Task 2: The `get_events` tool

**Files:**
- Create: `internal/mcp/events.go`
- Modify: `internal/mcp/tools.go` (add to `toolCatalog()`)
- Test: `internal/mcp/events_test.go`

**Interfaces:**
- Consumes: `cluster.TimelineEntry`, `cluster.SortTimeline` from Task 1.
- Produces: `eventsResult` struct with fields `Entries []cluster.TimelineEntry`, `Total int`, `Truncated bool`.

`cache.HistoryQuery` already supports `Type`, `ResourceID`, `BeforeID`, `NameContains` and `Limit` — `internal/api/feed_handlers.go` uses all of them. The MCP resource `cetacean://history` discards every one and serves a fixed 100 newest entries, which on a restarting cluster is about seven minutes of wall-clock. This tool is that query surfaced.

Note the resource keeps its fixed window and stays subscribable; this tool is the filterable form, exactly as `get_recommendations` is the tool form of `cetacean://recommendations`.

- [ ] **Step 1: Write the failing test**

```go
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
```

Add this helper to `internal/mcp/testsupport_test.go` if it is not already present:

```go
// toolRequest builds the CallToolRequest a handler sees, so a tool test
// exercises the same argument decoding a real call does.
func toolRequest(t *testing.T, name string, args map[string]any) mcplib.CallToolRequest {
	t.Helper()

	var req mcplib.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args

	return req
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestGetEvents`
Expected: FAIL — `undefined: eventsResult`, `undefined: toolGetEvents`.

- [ ] **Step 3: Write the tool**

```go
package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// maxEventLimit bounds one read. The ring holds 10,000 entries; handing all of
// them to a model is the failure cetacean://history already demonstrates.
const (
	maxEventLimit     = 500
	defaultEventLimit = 100
)

// eventsResult is what get_events answers with.
type eventsResult struct {
	Entries []cluster.TimelineEntry `json:"entries"`
	Total   int                     `json:"total"`

	// Truncated says the window held more than Limit, so a caller narrowing
	// by time knows it is looking at a page rather than the whole answer.
	Truncated bool `json:"truncated"`
}

// toolGetEvents serves the change timeline, filtered.
//
// cetacean://history exists and stays: it is subscribable, and a resource is
// the only thing a client can subscribe to. What it cannot do is answer a
// question — it serves a fixed 100 newest entries, which on a cluster with a
// restarting service is minutes of wall-clock and is all task churn. Every
// filter this tool takes is one cache.HistoryQuery already supports and the
// Atom feeds already use; none of them were reachable from the tool surface.
func (s *Server) toolGetEvents(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	since, err := optionalTime(req, "since")
	if err != nil {
		return "", err
	}

	until, err := optionalTime(req, "until")
	if err != nil {
		return "", err
	}

	limit := req.GetInt("limit", defaultEventLimit)
	if limit <= 0 || limit > maxEventLimit {
		limit = maxEventLimit
	}

	wanted := requestedTypes(req)

	// Over-read, because the type and time filters are applied after the ring
	// returns and would otherwise silently shorten the page.
	entries := s.cache.History().List(cache.HistoryQuery{
		ResourceID: req.GetString("resource", ""),
		Limit:      maxEventLimit * 4,
	})
	entries = s.filterHistory(ctx, entries)
	entries = nameHistoryTasks(s.cache, entries)

	timeline := make([]cluster.TimelineEntry, 0, len(entries))

	for _, e := range entries {
		if len(wanted) > 0 && !wanted[string(e.Type)] {
			continue
		}
		if !since.IsZero() && !e.Timestamp.After(since) {
			continue
		}
		if !until.IsZero() && e.Timestamp.After(until) {
			continue
		}

		timeline = append(timeline, cluster.TimelineEntry{
			At:         e.Timestamp.UTC().Format(time.RFC3339Nano),
			Kind:       "change",
			Type:       string(e.Type),
			Name:       e.Name,
			ResourceID: e.ResourceID,
			Message:    e.Action,
		})
	}

	cluster.SortTimeline(timeline)

	total := len(timeline)
	truncated := total > limit
	if truncated {
		timeline = timeline[:limit]
	}

	return marshalResult(eventsResult{
		Entries:   timeline,
		Total:     total,
		Truncated: truncated,
	})
}

// optionalTime parses an RFC 3339 argument, naming the format when it cannot.
// A model that guessed "yesterday" has to be told what is accepted.
func optionalTime(req mcplib.CallToolRequest, arg string) (time.Time, error) {
	raw := req.GetString(arg, "")
	if raw == "" {
		return time.Time{}, nil
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%s: %q is not an RFC 3339 timestamp (e.g. 2026-09-05T14:02:00Z)",
			arg, raw,
		)
	}

	return parsed, nil
}

// requestedTypes reads the `types` array into a set, or returns nil for "all".
func requestedTypes(req mcplib.CallToolRequest) map[string]bool {
	raw := req.GetStringSlice("types", nil)
	if len(raw) == 0 {
		return nil
	}

	set := make(map[string]bool, len(raw))
	for _, t := range raw {
		set[t] = true
	}

	return set
}
```

- [ ] **Step 4: Register the tool in `toolCatalog()`**

Add to the tier-0 block in `internal/mcp/tools.go`, beside `get_recommendations`:

```go
{
	tool: mcplib.NewTool(
		"get_events",
		mcplib.WithToolTitle("Read the cluster change timeline"),
		mcplib.WithDescription(
			"Return what changed in the cluster and when — resource creates, updates and deletes — newest first, narrowed by time, type or a single resource. Use it to answer what happened overnight, whether a fault is new, and what else changed around the moment something broke; pair it with get_logs, which returns the same entry shape, to read changes and output on one timeline. Filtering by `types` is usually necessary: a service restarting in a loop produces a task event every few seconds and buries everything else.",
		),
		mcplib.WithOutputSchema[eventsResult](),
		mcplib.WithReadOnlyHintAnnotation(true),
		mcplib.WithDestructiveHintAnnotation(false),
		mcplib.WithString("since", mcplib.Description("RFC 3339 timestamp; only entries strictly after this time.")),
		mcplib.WithString("until", mcplib.Description("RFC 3339 timestamp; only entries at or before this time.")),
		mcplib.WithArray("types",
			mcplib.Description("Resource types to include: node, service, task, stack, config, secret, network, volume. Omit for all."),
			mcplib.Items(map[string]any{"type": "string"}),
		),
		mcplib.WithString("resource", mcplib.Description("Only entries for this resource ID.")),
		mcplib.WithNumber("limit", mcplib.Description("Maximum entries to return (default 100, maximum 500).")),
	),
	tier:    config.OpsReadOnly,
	handler: s.toolGetEvents,
},
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -run TestGetEvents`
Expected: PASS

Then the whole package, to catch the registry-walking tests (`TestEveryToolAdvertisesOutputSchema`, the icon map, the prompt coverage test):

Run: `go test ./internal/mcp/`
Expected: PASS. If the icon test fails, add `"get_events": "read"` to `toolIconCategory` in `internal/mcp/tools.go`.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/events.go internal/mcp/events_test.go internal/mcp/tools.go internal/mcp/testsupport_test.go
git commit -m "feat(mcp): add get_events, the change timeline as a filterable tool"
```

---

### Task 3: `get_cluster_status`

**Files:**
- Create: `internal/cluster/status.go`, `internal/mcp/status.go`
- Modify: `internal/mcp/tools.go`
- Test: `internal/cluster/status_test.go`, `internal/mcp/status_test.go`

**Interfaces:**
- Consumes: `cluster.Row`, `cluster.RowsForServices`, `cluster.RowsForNodes`, `cache.ClusterSnapshot`.
- Produces: `cluster.ClusterStatus`; `cluster.BuildClusterStatus(snap cache.ClusterSnapshot, services []swarm.Service, nodes []swarm.Node, running map[string]int) ClusterStatus`.

The landing call. `cetacean://cluster` counts unhealthy things — `servicesDegraded: 3` — and counting is the half that guarantees a follow-up call. This names them.

- [ ] **Step 1: Write the failing test**

```go
package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// The point of the landing call: it names what is wrong. A count forces the
// caller to spend a second call finding out which, which is where the token
// budget actually goes.
func TestBuildClusterStatusNamesUnhealthyServices(t *testing.T) {
	one := uint64(1)
	broken := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "demo_stuck"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	}
	healthy := swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	}

	got := BuildClusterStatus(
		cache.ClusterSnapshot{},
		[]swarm.Service{broken, healthy},
		nil,
		map[string]int{"svc2": 1},
	)

	if len(got.UnhealthyServices) != 1 {
		t.Fatalf("unhealthy = %d, want 1", len(got.UnhealthyServices))
	}
	if got.UnhealthyServices[0].Name != "demo_stuck" {
		t.Errorf("name = %q, want demo_stuck", got.UnhealthyServices[0].Name)
	}
	if got.Healthy {
		t.Error("Healthy is true with a failed service")
	}
}

func TestBuildClusterStatusReportsHealthyWhenNothingIsWrong(t *testing.T) {
	one := uint64(1)
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	}

	got := BuildClusterStatus(
		cache.ClusterSnapshot{},
		[]swarm.Service{svc},
		nil,
		map[string]int{"svc1": 1},
	)

	if !got.Healthy {
		t.Errorf("Healthy = false with nothing wrong: %+v", got)
	}
	if len(got.UnhealthyServices) != 0 {
		t.Errorf("unhealthy = %+v, want empty", got.UnhealthyServices)
	}
}

// A node that is down or draining is the other half of "is the cluster ok".
func TestBuildClusterStatusNamesUnhealthyNodes(t *testing.T) {
	down := swarm.Node{
		ID:          "n1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateDown},
	}
	ready := swarm.Node{
		ID:          "n2",
		Description: swarm.NodeDescription{Hostname: "worker-2"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		Spec:        swarm.NodeSpec{Availability: swarm.NodeAvailabilityActive},
	}

	got := BuildClusterStatus(cache.ClusterSnapshot{}, nil, []swarm.Node{down, ready}, nil)

	if len(got.UnhealthyNodes) != 1 {
		t.Fatalf("unhealthy nodes = %d, want 1", len(got.UnhealthyNodes))
	}
	if got.UnhealthyNodes[0].Name != "worker-1" {
		t.Errorf("name = %q, want worker-1", got.UnhealthyNodes[0].Name)
	}
}

// Capacity in cores, both figures in the same unit, for the same reason
// cetacean://cluster was projected: reserved over total must be divisible.
func TestBuildClusterStatusReportsCapacityInCores(t *testing.T) {
	got := BuildClusterStatus(cache.ClusterSnapshot{
		TotalCPU:       12,
		ReservedCPU:    2_350_000_000,
		TotalMemory:    16 << 30,
		ReservedMemory: 2 << 30,
	}, nil, nil, nil)

	if got.Capacity.ReservedCPUCores != 2.35 {
		t.Errorf("ReservedCPUCores = %v, want 2.35", got.Capacity.ReservedCPUCores)
	}
	if got.Capacity.TotalCPUCores != 12 {
		t.Errorf("TotalCPUCores = %v, want 12", got.Capacity.TotalCPUCores)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cluster/ -run TestBuildClusterStatus`
Expected: FAIL — `undefined: BuildClusterStatus`.

- [ ] **Step 3: Write the status builder**

```go
package cluster

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// ClusterStatus is the landing read: everything needed to decide whether to
// look further, and where.
//
// It names the unhealthy resources rather than counting them.
// cache.ClusterSnapshot reports "servicesDegraded": 3, which is an answer that
// guarantees a second call — and follow-up calls are where a caller's budget
// actually goes. Every unhealthy entry is a Row, so it carries the id and name
// the next describe needs.
type ClusterStatus struct {
	// Healthy is the one-line answer: nothing degraded, nothing unreachable,
	// nothing mid-rollout that has stalled.
	Healthy bool `json:"healthy"`

	NodeCount    int `json:"nodeCount"`
	ServiceCount int `json:"serviceCount"`
	TaskCount    int `json:"taskCount"`
	StackCount   int `json:"stackCount"`

	// UnhealthyServices are the services not in their desired state, and
	// Rollouts those mid-update. Both are always arrays, never null.
	UnhealthyServices []Row `json:"unhealthyServices"`
	UnhealthyNodes    []Row `json:"unhealthyNodes"`
	Rollouts          []Row `json:"rollouts"`

	Capacity ClusterCapacity `json:"capacity"`
}

// ClusterCapacity is the reserved-versus-available picture, in named units.
//
// Both CPU figures are cores because the snapshot behind them holds one in
// cores and one in nanoCPUs, and a caller dividing them as the snapshot spells
// them is wrong by nine orders of magnitude.
type ClusterCapacity struct {
	TotalCPUCores       float64 `json:"totalCPUCores"`
	ReservedCPUCores    float64 `json:"reservedCPUCores"`
	TotalMemoryBytes    int64   `json:"totalMemoryBytes"`
	ReservedMemoryBytes int64   `json:"reservedMemoryBytes"`
}

// nanoCPUsPerCore is Docker's fixed-point scale: NanoCPUs of 1e9 is one core.
const nanoCPUsPerCore = 1e9

// BuildClusterStatus assembles the landing view.
//
// services and nodes are the caller's already-ACL-filtered slices, and running
// the running-task count per service, so a caller with grants over one stack is
// told about their cluster rather than everyone's — the same rule every builder
// in this package follows.
func BuildClusterStatus(
	snap cache.ClusterSnapshot,
	services []swarm.Service,
	nodes []swarm.Node,
	running map[string]int,
) ClusterStatus {
	status := ClusterStatus{
		NodeCount:         snap.NodeCount,
		ServiceCount:      snap.ServiceCount,
		TaskCount:         snap.TaskCount,
		StackCount:        snap.StackCount,
		UnhealthyServices: []Row{},
		UnhealthyNodes:    []Row{},
		Rollouts:          []Row{},
		Capacity: ClusterCapacity{
			TotalCPUCores:       float64(snap.TotalCPU),
			ReservedCPUCores:    float64(snap.ReservedCPU) / nanoCPUsPerCore,
			TotalMemoryBytes:    snap.TotalMemory,
			ReservedMemoryBytes: snap.ReservedMemory,
		},
	}

	for _, row := range RowsForServices(services, running) {
		switch row.State {
		case "failed", "pending":
			status.UnhealthyServices = append(status.UnhealthyServices, row)
		case "updating":
			status.Rollouts = append(status.Rollouts, row)
		}
	}

	for _, row := range RowsForNodes(nodes) {
		if row.State != "ready" {
			status.UnhealthyNodes = append(status.UnhealthyNodes, row)
		}
	}

	status.Healthy = len(status.UnhealthyServices) == 0 && len(status.UnhealthyNodes) == 0

	return status
}
```

**Note:** `nanoCPUsPerCore` already exists as an unexported constant in `internal/mcp/cluster_resource.go`. Two packages may each hold their own; do not export one to share it, since the MCP one is about a wire projection and this one about a domain figure. If the linter objects to the duplicate name across packages it is mistaken — they are in different packages.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cluster/ -run TestBuildClusterStatus`
Expected: PASS

- [ ] **Step 5: Write the MCP tool and its test**

`internal/mcp/status.go`:

```go
package mcp

import (
	"context"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// toolGetClusterStatus is the landing call.
//
// It reads through the same ACL-filtered listings find uses rather than the
// cache directly, so a caller is told about the cluster they can see. The
// counts still come from the snapshot, which is cluster-wide — they are
// aggregates a caller with partial grants can already read at
// cetacean://cluster, and withholding them would make the two disagree.
func (s *Server) toolGetClusterStatus(
	ctx context.Context,
	_ mcplib.CallToolRequest,
) (string, error) {
	services := s.filterServices(ctx, s.cache.ListServices())
	nodes := s.filterNodes(ctx, s.cache.ListNodes())

	return marshalResult(cluster.BuildClusterStatus(
		s.cache.Snapshot(),
		services,
		nodes,
		s.cache.RunningTaskCounts(),
	))
}
```

`internal/mcp/status_test.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// The whole surface's landing question, end to end: one call, and the broken
// thing is named rather than counted.
func TestGetClusterStatusNamesTheBrokenService(t *testing.T) {
	c := cache.New(nil)
	one := uint64(1)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "demo_stuck"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.toolGetClusterStatus(context.Background(), toolRequest(t, "get_cluster_status", nil))
	if err != nil {
		t.Fatalf("toolGetClusterStatus: %v", err)
	}

	var got cluster.ClusterStatus
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Healthy {
		t.Error("Healthy = true with a service that has no running replica")
	}
	if len(got.UnhealthyServices) != 1 || got.UnhealthyServices[0].Name != "demo_stuck" {
		t.Errorf("unhealthyServices = %+v, want one named demo_stuck", got.UnhealthyServices)
	}
}

// Empty arrays, never null — the rule every schema-advertising type here
// follows, and the one a strict client trips over.
func TestGetClusterStatusMarshalsEmptyArrays(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	body, err := srv.toolGetClusterStatus(context.Background(), toolRequest(t, "get_cluster_status", nil))
	if err != nil {
		t.Fatalf("toolGetClusterStatus: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, field := range []string{"unhealthyServices", "unhealthyNodes", "rollouts"} {
		if raw[field] == nil {
			t.Errorf("%s marshalled as null, want []", field)
		}
	}
}
```

- [ ] **Step 6: Register the tool**

Add to the tier-0 block in `toolCatalog()`:

```go
{
	tool: mcplib.NewTool(
		"get_cluster_status",
		mcplib.WithToolTitle("Check overall cluster health"),
		mcplib.WithDescription(
			"Answer whether the cluster is healthy and, when it is not, name what is wrong: the services not in their desired state, the nodes that are down or draining, the rollouts in flight, and how much CPU and memory is reserved against what the cluster has. Start here — it is the one call that says whether to look further and where, and every entry carries the id and name to describe next.",
		),
		mcplib.WithOutputSchema[cluster.ClusterStatus](),
		mcplib.WithReadOnlyHintAnnotation(true),
		mcplib.WithDestructiveHintAnnotation(false),
	),
	tier:    config.OpsReadOnly,
	handler: s.toolGetClusterStatus,
},
```

Add `"get_cluster_status": "read"` to `toolIconCategory`.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/cluster/ ./internal/mcp/`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/cluster/status.go internal/cluster/status_test.go internal/mcp/status.go internal/mcp/status_test.go internal/mcp/tools.go
git commit -m "feat(mcp): add get_cluster_status, which names what is broken"
```

---

### Task 4: Cluster and stack log scopes

**Files:**
- Create: `internal/mcp/logs_scope.go`
- Modify: `internal/mcp/logs.go` (`optsFromToolRequest` gains `contains`), `internal/mcp/tools.go` (`get_logs` schema)
- Test: `internal/mcp/logs_scope_test.go`

**Interfaces:**
- Consumes: `Server.readLogsImpl`, `logOptions` from `internal/mcp/logs.go`.
- Produces: `Server.readScopedLogs(ctx context.Context, scope string, target string, opts logOptions) (LogResourceResponse, error)`.

Intents 24–26 — anything cluster-wide in five minutes, every error in the last hour, grep the cluster — are today enumerate-then-fan-out, driven by the model and paid for in its context. The server can do the fan-out in-process.

- [ ] **Step 1: Write the failing test**

```go
package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// The saving this task exists for: one call instead of one per service, with
// the merge done server-side.
func TestClusterScopeReadsEveryService(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"web", "api"} {
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	// One line per service, merged into one response.
	if len(got.Lines) != 2 {
		t.Fatalf("lines = %d, want 2 (one per service)", len(got.Lines))
	}
}

// Every line must say which service it came from, or a merged read is
// unreadable.
func TestClusterScopeAttributesEachLine(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if got.Lines[0].Attrs["serviceName"] != "web" {
		t.Errorf("serviceName = %v, want web", got.Lines[0].Attrs["serviceName"])
	}
}

// grep-the-cluster, done server-side: the caller pays for matches, not for
// every line of every service.
func TestContainsFiltersServerSide(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	streamer := &fakeLogStreamer{
		frames: append(
			buildLogFrame(1, "2026-09-05T14:00:00.000000000Z connection refused\n"),
			buildLogFrame(1, "2026-09-05T14:00:01.000000000Z all good\n")...,
		),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(
		context.Background(), "cluster", "",
		logOptions{tail: 10, contains: "refused"},
	)
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("lines = %d, want 1 match", len(got.Lines))
	}
	if !strings.Contains(got.Lines[0].Message, "refused") {
		t.Errorf("kept the wrong line: %q", got.Lines[0].Message)
	}
}

// A stack scope reads only its own members.
func TestStackScopeReadsOnlyItsMembers(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
			Name:   "demo_web",
			Labels: map[string]string{"com.docker.stack.namespace": "demo"},
		}},
	})
	c.SetService(swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
			Name:   "other_api",
			Labels: map[string]string{"com.docker.stack.namespace": "other"},
		}},
	})

	streamer := &fakeLogStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readScopedLogs(context.Background(), "stack", "demo", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("lines = %d, want 1 (only the demo stack's service)", len(got.Lines))
	}
	if got.Lines[0].Attrs["serviceName"] != "demo_web" {
		t.Errorf("read the wrong service: %v", got.Lines[0].Attrs["serviceName"])
	}
}

// One unreadable service must not fail the whole read: a cluster-wide grep
// that dies on the first broken service is worse than no tool.
func TestScopedReadSurvivesOneFailingService(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"web", "api"} {
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	srv := newLogTestServer(t, c, &failOnceStreamer{
		frames: buildLogFrame(1, "2026-09-05T14:00:00.000000000Z hello\n"),
	})

	got, err := srv.readScopedLogs(context.Background(), "cluster", "", logOptions{tail: 10})
	if err != nil {
		t.Fatalf("readScopedLogs: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Errorf("lines = %d, want the one service that worked", len(got.Lines))
	}
	if len(got.Errors) != 1 {
		t.Errorf("errors = %d, want the one that failed reported", len(got.Errors))
	}
}
```

Add this stub to `internal/mcp/logs_scope_test.go`:

```go
// failOnceStreamer fails the first read and serves frames thereafter, so a
// fan-out test can assert partial success rather than all-or-nothing.
type failOnceStreamer struct {
	frames []byte
	failed bool
	mu     sync.Mutex
}

func (f *failOnceStreamer) Logs(
	_ context.Context,
	_ docker.LogKind,
	_, _ string,
	_ bool,
	_, _ string,
) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.failed {
		f.failed = true
		return nil, errors.New("docker unavailable")
	}

	return io.NopCloser(bytes.NewReader(f.frames)), nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run 'TestClusterScope|TestContainsFilters|TestStackScope|TestScopedRead'`
Expected: FAIL — `undefined: readScopedLogs`, `logOptions has no field contains`, `LogResourceResponse has no field Errors`.

- [ ] **Step 3: Add `contains` to `logOptions` and `Errors` to the response**

In `internal/mcp/logs.go`, add to the `logOptions` struct:

```go
	// contains narrows to lines holding this substring, case-insensitively.
	// Applied server-side because the point of a cluster-wide read is that the
	// caller pays for matches rather than for every line of every service.
	contains string
```

and to `optsFromToolRequest`:

```go
	opts.contains = req.GetString("contains", "")
```

Add to `LogResourceResponse`:

```go
	// Errors names the services a scoped read could not reach, so a partial
	// answer says which part is missing rather than pretending to be whole.
	// Always an array, never null.
	Errors []string `json:"errors,omitempty"`
```

Apply the filter in `readLogsImpl`, immediately after the existing `filterLogLines` call:

```go
	lines = filterLogContains(lines, opts.contains)
```

- [ ] **Step 4: Write the fan-out**

```go
package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/logs"
)

const (
	// maxScopedServices bounds a fan-out. A cluster-wide read of a hundred
	// services is not a question anyone is asking; it is a way to exhaust a
	// context window.
	maxScopedServices = 25

	// scopedTailPerService is the per-service cap. The caller's `tail` is the
	// cap on the merged result, not on each service, or a twenty-service read
	// would return twenty times what was asked for.
	scopedTailPerService = 50

	// scopedConcurrency bounds simultaneous Docker log reads.
	scopedConcurrency = 4
)

// filterLogContains narrows lines to those holding sub, case-insensitively.
// An empty sub keeps everything.
func filterLogContains(lines []logs.LogLine, sub string) []logs.LogLine {
	if sub == "" {
		return lines
	}

	needle := strings.ToLower(sub)
	kept := make([]logs.LogLine, 0, len(lines))

	for _, line := range lines {
		if strings.Contains(strings.ToLower(line.Message), needle) {
			kept = append(kept, line)
		}
	}

	return kept
}

// readScopedLogs merges the output of every service in a scope.
//
// Intents 24-26 — "anything cluster-wide in the last five minutes", "every
// error in the last hour", "grep the cluster" — are enumerate-then-fan-out
// when the caller does them: one list call, then one log call per service,
// each round-trip paid for in the model's context. The join costs the server
// almost nothing and the caller one call.
//
// A service that cannot be read is reported in Errors rather than failing the
// whole read: a cluster-wide grep that dies on the first unreachable service
// is worse than no tool at all.
func (s *Server) readScopedLogs(
	ctx context.Context,
	scope string,
	target string,
	opts logOptions,
) (LogResourceResponse, error) {
	if s.logs == nil {
		return LogResourceResponse{Lines: []logs.LogLine{}, Errors: []string{}}, nil
	}

	services, err := s.servicesInScope(ctx, scope, target)
	if err != nil {
		return LogResourceResponse{}, err
	}

	perService := opts
	perService.tail = scopedTailPerService

	var (
		mu       sync.Mutex
		merged   []logs.LogLine
		failures []string
		wg       sync.WaitGroup
	)

	sem := make(chan struct{}, scopedConcurrency)

	for _, svc := range services {
		wg.Add(1)

		go func(svc swarm.Service) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			resp, err := s.readLogsImpl(ctx, docker.ServiceLog, svc.ID, perService)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", svc.Spec.Name, err))

				return
			}

			for _, line := range resp.Lines {
				if line.Attrs == nil {
					line.Attrs = map[string]string{}
				}
				line.Attrs["serviceName"] = svc.Spec.Name
				line.Attrs["serviceId"] = svc.ID

				merged = append(merged, line)
			}
		}(svc)
	}

	wg.Wait()

	// Newest last, so the tail cut below keeps the newest lines.
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Timestamp < merged[j].Timestamp
	})

	wanted := opts.tail
	if wanted <= 0 {
		wanted = defaultLogTail
	}
	if len(merged) > wanted {
		merged = merged[len(merged)-wanted:]
	}

	sort.Strings(failures)

	if merged == nil {
		merged = []logs.LogLine{}
	}
	if failures == nil {
		failures = []string{}
	}

	resp := LogResourceResponse{Lines: merged, Errors: failures}
	if len(merged) > 0 {
		resp.Cursor = merged[len(merged)-1].Timestamp
	}

	return resp, nil
}

// servicesInScope resolves a scope to the services it covers, already filtered
// to what the caller may read — so a cluster-wide grep cannot become a way to
// read output from a service the caller has no grant for.
func (s *Server) servicesInScope(
	ctx context.Context,
	scope string,
	target string,
) ([]swarm.Service, error) {
	all := s.filterServices(ctx, s.cache.ListServices())

	var picked []swarm.Service

	switch scope {
	case "cluster":
		picked = all

	case "stack":
		if target == "" {
			return nil, fmt.Errorf("stack: a stack name is required")
		}

		for _, svc := range all {
			if strings.EqualFold(svc.Spec.Labels["com.docker.stack.namespace"], target) {
				picked = append(picked, svc)
			}
		}

		if len(picked) == 0 {
			return nil, fmt.Errorf("stack %q has no services you can read", target)
		}

	default:
		return nil, fmt.Errorf("scope %q: want \"cluster\" or \"stack\"", scope)
	}

	// Sorted so the per-service cap below is deterministic rather than
	// whichever services the map happened to yield first.
	sort.Slice(picked, func(i, j int) bool {
		return picked[i].Spec.Name < picked[j].Spec.Name
	})

	if len(picked) > maxScopedServices {
		picked = picked[:maxScopedServices]
	}

	return picked, nil
}
```

- [ ] **Step 5: Wire the scopes into the `get_logs` handler**

In `internal/mcp/tools.go`, in the `get_logs` handler (around line 956), before the existing service/task branch:

```go
	if stack := req.GetString("stack", ""); stack != "" {
		resp, err := s.readScopedLogs(ctx, "stack", stack, optsFromToolRequest(req))
		if err != nil {
			return "", err
		}

		return marshalResult(resp)
	}

	if req.GetBool("cluster", false) {
		resp, err := s.readScopedLogs(ctx, "cluster", "", optsFromToolRequest(req))
		if err != nil {
			return "", err
		}

		return marshalResult(resp)
	}
```

Extend the `get_logs` tool declaration with:

```go
	mcplib.WithString("stack", mcplib.Description("Stack name; merges the logs of every service in it. Mutually exclusive with service and task.")),
	mcplib.WithBoolean("cluster", mcplib.Description("Read every service in the cluster, merged. Mutually exclusive with service, task and stack.")),
	mcplib.WithString("contains", mcplib.Description("Keep only lines containing this substring (case-insensitive). Applied server-side, so a wide read costs the matches rather than every line.")),
```

and replace its description with:

```
"Fetch recent log lines from one service, one task, a whole stack, or the whole cluster. Name exactly one scope: `service` merges the output of every live replica; `task` reads a single replica and is the only way to reach one that has already exited; `stack` and `cluster` merge across services server-side, so grepping the cluster is one call rather than one per service, and every line says which service it came from. Swarm discards a task's output once its record falls out of the history window (five per replica slot by default), so on a service restarting in a loop that history is only seconds deep and should be read first. Narrow with `contains` for a substring, `level` for a minimum severity and `since` for a start time — on a wide read `contains` is what keeps the answer small. A wide read covers at most 25 services and reports any it could not reach in `errors` rather than failing. This is a one-shot read; for live tails subscribe to the cetacean://services/{id}/logs resource."
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/mcp/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/logs_scope.go internal/mcp/logs_scope_test.go internal/mcp/logs.go internal/mcp/tools.go
git commit -m "feat(mcp): read logs across a stack or the whole cluster, with server-side grep"
```

---

### Task 5: The `watch` tool

**Files:**
- Create: `internal/mcp/watch.go`
- Modify: `internal/mcp/tools.go`, `internal/mcp/server.go` (`toolACLSpecs`)
- Test: `internal/mcp/watch_test.go`

**Interfaces:**
- Consumes: `Server.awaitConvergence`, `convergenceTimeout` from `internal/mcp/tasks.go`; `cluster.ServiceConverged`.
- Produces: `watchResult` struct with `Outcome string`, `Observed string`, `ElapsedSeconds float64`.

The convergence rule exists and is correct, but is reachable only as a side effect of a mutation — an agent cannot ask "has it deployed yet?" without deploying something. This makes it a read, turning intent 18 from a twenty-poll loop into one call.

The same detached-context caveat as the converging mutations applies: `awaitServiceConvergence` detaches from the request context, so `tasks/cancel` cannot interrupt the wait and `timeout` is the real bound. Say so in the description.

- [ ] **Step 1: Write the failing test**

```go
package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// A service already in its desired state returns at once — the common case,
// and the one that must not cost a timeout.
func TestWatchReturnsImmediatelyWhenAlreadyConverged(t *testing.T) {
	c := cache.New(nil)
	one := uint64(1)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	})
	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.toolWatch(context.Background(), toolRequest(t, "watch", map[string]any{
		"service": "web",
		"timeout": float64(2),
	}))
	if err != nil {
		t.Fatalf("toolWatch: %v", err)
	}

	var got watchResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Outcome != "converged" {
		t.Errorf("outcome = %q, want converged", got.Outcome)
	}
}

// A service that never converges must report why, and must respect the
// caller's timeout rather than the five-minute default.
func TestWatchTimesOutWithTheLastProgressLine(t *testing.T) {
	c := cache.New(nil)
	three := uint64(3)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &three}},
		},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.toolWatch(context.Background(), toolRequest(t, "watch", map[string]any{
		"service": "web",
		"timeout": float64(1),
	}))
	if err != nil {
		t.Fatalf("toolWatch: %v", err)
	}

	var got watchResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Outcome != "timeout" {
		t.Errorf("outcome = %q, want timeout", got.Outcome)
	}
	if got.Observed == "" {
		t.Error("Observed is empty; a timeout must say how far it got")
	}
}

func TestWatchRejectsAnUnknownService(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	_, err := srv.toolWatch(context.Background(), toolRequest(t, "watch", map[string]any{
		"service": "nosuch",
	}))
	if err == nil {
		t.Fatal("expected an error for an unknown service")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestWatch`
Expected: FAIL — `undefined: watchResult`, `undefined: toolWatch`.

- [ ] **Step 3: Write the tool**

```go
package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// maxWatchTimeout bounds a wait. convergenceTimeout (5m) is the ceiling the
// converging mutations already use; a read has no reason to exceed it.
const (
	maxWatchTimeout     = 5 * time.Minute
	defaultWatchTimeout = 60 * time.Second
)

// watchResult reports how a wait ended.
type watchResult struct {
	// Outcome is "converged" or "timeout".
	Outcome string `json:"outcome"`

	// Observed is the last progress line the convergence check produced —
	// "waiting: 1/3 replicas running". On a timeout it is the whole answer:
	// it says how far the rollout actually got.
	Observed string `json:"observed"`

	ElapsedSeconds float64 `json:"elapsedSeconds"`
}

// toolWatch waits until a service has settled, and reports what it saw.
//
// cluster.ServiceConverged is the rule for "settled", and REST and MCP already
// share it — but it was reachable only as a side effect of a mutation, so an
// agent could not ask "has it deployed yet?" without deploying something.
// Making it a read turns a poll loop of twenty describe calls into one call.
//
// The wait detaches from the request context, exactly as the converging
// mutations do, because a task-augmented call runs on a goroutine holding an
// already-cancelled HTTP context. tasks/cancel therefore cannot interrupt it;
// `timeout` is the real bound, which is why it is capped.
func (s *Server) toolWatch(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	name := req.GetString("service", "")
	if name == "" {
		return "", fmt.Errorf("service: required")
	}

	svc, found, err := s.cache.ResolveService(name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("no such service %q", name)
	}

	if err := s.checkRead(ctx, "service", svc.Spec.Name); err != nil {
		return "", err
	}

	timeout := time.Duration(req.GetInt("timeout", int(defaultWatchTimeout.Seconds()))) * time.Second
	if timeout <= 0 || timeout > maxWatchTimeout {
		timeout = maxWatchTimeout
	}

	started := time.Now()

	result := watchResult{Outcome: "converged"}

	waitErr := s.awaitServiceConvergenceFor(ctx, svc.ID, timeout, &result.Observed)
	if waitErr != nil {
		result.Outcome = "timeout"
	}

	result.ElapsedSeconds = time.Since(started).Seconds()

	return marshalResult(result)
}
```

**Note for the implementer:** `awaitServiceConvergenceFor` does not exist yet. `internal/mcp/tasks.go` has `awaitServiceConvergence`, which hard-codes `convergenceTimeout` and returns early unless the call is task-augmented. Extract the waiting core into a helper both can call:

```go
// awaitServiceConvergenceFor waits up to timeout for svcID to settle, writing
// the last progress line into observed. It is the core awaitServiceConvergence
// wraps; watch needs the same wait with a caller-chosen bound and without the
// task-augmentation early return.
func (s *Server) awaitServiceConvergenceFor(
	ctx context.Context,
	svcID string,
	timeout time.Duration,
	observed *string,
) error {
	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	return s.awaitConvergence(detached, func() (bool, string) {
		done, progress := s.serviceConverged(svcID)
		if observed != nil {
			*observed = progress
		}

		return done, progress
	})
}
```

Then rewrite `awaitServiceConvergence` to call it with `convergenceTimeout`, so there is one wait rather than two. Run `go test ./internal/mcp/ -run TestTask` afterwards — the tasks tests are the regression net for that refactor.

- [ ] **Step 4: Register the tool**

Add to the tier-0 block in `toolCatalog()`:

```go
{
	tool: mcplib.NewTool(
		"watch",
		mcplib.WithToolTitle("Wait for a service to settle"),
		mcplib.WithDescription(
			"Block until a service has reached its desired state — every replica running and no rolling update in flight — then report whether it settled and how long it took. Use it after a deploy or a scale instead of describing the service in a loop. On a timeout it reports how far the rollout actually got (\"waiting: 1/3 replicas running\"), which is the answer when a deploy is stuck. The wait cannot be cancelled once started, so `timeout` is the real bound; it defaults to 60 seconds and is capped at 5 minutes.",
		),
		mcplib.WithOutputSchema[watchResult](),
		mcplib.WithReadOnlyHintAnnotation(true),
		mcplib.WithDestructiveHintAnnotation(false),
		mcplib.WithString("service", mcplib.Required(), mcplib.Description("Service ID or name to wait on.")),
		mcplib.WithNumber("timeout", mcplib.Description("Seconds to wait before giving up (default 60, maximum 300).")),
	),
	tier:    config.OpsReadOnly,
	handler: s.toolWatch,
},
```

Add `"watch": "read"` to `toolIconCategory`, and to `toolACLSpecs` in `internal/mcp/server.go`:

```go
	"watch": {resourceType: "service", permission: "read"},
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/mcp/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/watch.go internal/mcp/watch_test.go internal/mcp/tasks.go internal/mcp/tools.go internal/mcp/server.go
git commit -m "feat(mcp): add watch, exposing the convergence rule as a read"
```

---

### Task 6: Documentation and changelog

**Files:**
- Modify: `docs/mcp.md`, `CHANGELOG.md`

- [ ] **Step 1: Document the four verbs in `docs/mcp.md`**

In the tier-0 tool section, after the `get_recommendations` paragraph, add:

```markdown
`get_cluster_status` is the landing call. It answers whether the cluster is healthy and, when it is not, **names** the
services and nodes that are wrong rather than counting them — every entry is a row carrying the id and name a
follow-up `describe` needs. `cetacean://cluster` still serves the raw aggregate for a client that wants to subscribe
to it; this is the form that answers a question.

`get_events` is the change timeline, filtered by time, resource type or a single resource. `cetacean://history` keeps
its fixed newest-100 window because a resource is the only thing a client can subscribe to, but that window is minutes
of wall-clock on a cluster with a restarting service, and all of it task churn — narrowing by `types` is usually
necessary. Entries share their shape with `get_logs`, so reading changes and output on one timeline is two calls
whose results interleave without translation. That is the answer to "this started at 14:02 — what else happened?"

`get_logs` reads one service, one task, a whole stack, or the whole cluster. The wide scopes merge server-side and
attribute every line to the service it came from, so grepping the cluster with `contains` costs one call and the
matches, rather than one call per service and every line of each. A wide read covers at most 25 services and lists
any it could not reach in `errors` rather than failing the whole read.

`watch` blocks until a service has settled and reports whether it did. The convergence rule is the same one the
converging mutations use — this exposes it as a read, so an agent can ask "has it deployed yet?" without deploying
something. The wait detaches from the request, so `tasks/cancel` cannot interrupt it and `timeout` is the real bound.
```

- [ ] **Step 2: Add the changelog entries**

Under `## [Unreleased]` → `### Added`:

```markdown
- AI agents can now ask whether the cluster is healthy and be told what is wrong, not just how many things are. The answer names the services and nodes that are degraded, the rollouts in flight, and how much capacity is reserved, so deciding where to look no longer costs a second round of calls
- AI agents can now ask what changed and when, narrowed to a time range, a resource type or a single resource. The change history was previously readable only as a fixed window of the hundred newest events, which on a cluster with a restarting service is a few minutes of one service's churn — so questions like "what broke overnight" could not be asked at all. Change events and log lines now share a shape, so an agent investigating an incident can read both on one timeline
- AI agents can now read logs across a whole stack or the whole cluster in one call, and search them for a substring, where before they had to list the services and then read each one separately. Every line says which service it came from, the search runs on the server so a wide read costs the matches rather than every line, and a service that cannot be read is reported rather than failing the request
- AI agents can now wait for a service to finish deploying and be told whether it did, instead of asking repeatedly. If it never settles, the answer says how far the rollout got
```

- [ ] **Step 3: Verify the whole suite and lint**

```bash
go test ./...
make lint
make fmt-check
```

Expected: all pass.

- [ ] **Step 4: Verify against a live cluster**

Follow `.claude/skills/run-cetacean/SKILL.md`. Build to a scratch path and run on a spare port so the user's running instance is untouched:

```bash
go build -o /tmp/cetacean-verify .
CETACEAN_MCP=true CETACEAN_LISTEN_ADDR=:9001 CETACEAN_LOG_FORMAT=text /tmp/cetacean-verify &
```

Drive the MCP endpoint over HTTP. The `2026-07-28` header contract is enforced strictly: every request needs `Mcp-Protocol-Version`, `Mcp-Method`, and `Mcp-Name` headers, plus `_meta` **inside `params`** carrying `io.modelcontextprotocol/protocolVersion`, `io.modelcontextprotocol/clientCapabilities` and `io.modelcontextprotocol/clientInfo`. Confirm each of:

- `get_cluster_status` names a genuinely broken service rather than counting it.
- `get_events` with `types: ["service"]` returns service events only, and with a `since` in the future returns none.
- `get_logs` with `cluster: true` and `contains` returns attributed, filtered lines from more than one service.
- `watch` on a healthy service returns `converged` promptly; on a service with unschedulable replicas it returns `timeout` with a progress line.

Kill the process and delete the binary afterwards.

- [ ] **Step 5: Commit**

```bash
git add docs/mcp.md CHANGELOG.md
git commit -m "docs(mcp): document the four new read verbs"
```

---

## Self-Review

**Spec coverage.** The spec's read-tool table lists eight rows. `describe`, `find` and `get_recommendations` shipped in plan 1 and are unchanged. This plan covers `get_cluster_status` (Task 3), `get_events` (Task 2), `get_logs` stack/cluster/`contains` (Task 4) and `watch` (Task 5). Two rows are **deliberately deferred and are not in this plan**: `get_metrics` gaining `target: "cluster"` and a `top` argument (intents 30, 33), and `get_topology` gaining `view: "drain-impact"` (intent 39). Both are additive to tools that already exist, neither blocks anything else, and the live cluster this was evaluated on has no working cAdvisor, so the metrics one cannot be verified end-to-end here. Raise them as a follow-up once cAdvisor reports per-container metrics.

**Type consistency.** `logOptions.contains` is added in Task 4 Step 3 and consumed in Step 4's `filterLogContains`; `LogResourceResponse.Errors` likewise. `cluster.TimelineEntry` is defined in Task 1 and consumed by `eventsResult` in Task 2. `watchResult.Observed` is filled through the `observed *string` out-parameter of `awaitServiceConvergenceFor`, defined in Task 5 Step 3. `cluster.Row` is used by `ClusterStatus` and comes from plan 1.

**Known refactor risk.** Task 5 rewrites `awaitServiceConvergence` in terms of a new `awaitServiceConvergenceFor`. `internal/mcp/tasks_test.go` and `tasks_e2e_test.go` are the regression net; run them explicitly.

**Assumption to check before starting Task 2.** `mcplib.CallToolRequest.GetStringSlice` is assumed to exist in `mark3labs/mcp-go` v1.0.0. If it does not, read the `types` argument via `req.GetArguments()["types"]` and type-assert `[]any`, converting each element with `fmt.Sprint`.
