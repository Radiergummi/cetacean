# MCP Compact Representation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace raw Docker Engine objects on MCP reads with a compact, self-describing representation, and merge `search` + `list_resources` into one `find` tool.

**Architecture:** Two new transport-neutral types in `internal/cluster` — `Row` (list unit) and `Digest` (detail unit) — built by functions that take the Docker objects the cache already holds. `internal/mcp` gets `find` and `describe` tools over them; the templated `cetacean://` resources keep their URIs and subscriptions but change payload to `Digest`. The table widget drops its per-type accessor table because every row already carries `id`/`name`/`state`/`detail`.

**Tech Stack:** Go 1.26, `mark3labs/mcp-go` v1.0.0, Docker Engine API types, React 19 + Vite for the widget.

**Spec:** `docs/superpowers/specs/2026-09-04-mcp-agent-surface-design.md`

## Global Constraints

- **A tool is the unit of visibility.** Collapse only within one operations tier and one target resource type. Never compute a tier at call time. (Spec: "The tier rule".)
- **Units are named, never implied.** Every numeric field carries its unit in its name (`memoryLimitBytes`, `cpuLimitCores`) or is a duration string (`"10s"`).
- **Env variable names only, never values.** Env carries credentials.
- **Secret data stays zeroed.** Reads go through `lookupResource`, which already redacts; do not bypass it.
- **Nil slices must not marshal to `null`.** Every slice in a type with an advertised output schema is initialised, per the existing `TopologyGraph` rule.
- **Sorted output.** Results are marshalled into MCP results a client may cache by ETag; range-over-map ordering must be sorted before returning.
- All new exported types and functions carry doc comments in the existing house style: say *why*, not *what*.

## Scope

This is plan 1 of 3 derived from the spec. It covers the compact representation, `find`, `describe`, and the widget migration. Plan 2 covers the new read verbs (`get_cluster_status`, `get_events`, `watch`, log scopes). Plan 3 covers the write changes (tier-2 merge, tier-3 secrets/mounts, creates).

---

### Task 1: The `Row` type and its builders

**Files:**
- Create: `internal/cluster/view.go`
- Create: `internal/cluster/view_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `cluster.Row` struct; `cluster.RowsForServices([]swarm.Service, []swarm.Task) []Row`, `cluster.RowsForNodes([]swarm.Node) []Row`, `cluster.RowsForTasks([]swarm.Task, []swarm.Service, []swarm.Node) []Row`, `cluster.RowsForStacks([]Stack) []Row`, `cluster.RowsForConfigs([]swarm.Config) []Row`, `cluster.RowsForSecrets([]swarm.Secret) []Row`, `cluster.RowsForNetworks([]network.Summary) []Row`, `cluster.RowsForVolumes([]*volume.Volume) []Row`.

- [ ] **Step 1: Write the failing test**

In `internal/cluster/view_test.go`:

```go
package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// A row must answer "what is this and is it healthy" without a second call:
// that is the whole reason the raw Docker object is not returned.
func TestRowsForServicesCarryDerivedState(t *testing.T) {
	svc := replicated("api", 3)
	svc.Spec.Labels = map[string]string{"com.docker.stack.namespace": "demo"}

	tasks := []swarm.Task{
		{ID: "t1", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
		{ID: "t2", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
	}

	rows := RowsForServices([]swarm.Service{svc}, tasks)

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}

	got := rows[0]

	if got.ID != "svc-api" || got.Name != "api" {
		t.Errorf("identity = %q/%q, want svc-api/api", got.ID, got.Name)
	}
	if got.Type != "service" {
		t.Errorf("type = %q, want service", got.Type)
	}
	if got.Stack != "demo" {
		t.Errorf("stack = %q, want demo", got.Stack)
	}
	if got.Detail != "api:1" {
		t.Errorf("detail = %q, want the image without its digest", got.Detail)
	}
	if got.Desired != 3 || got.Running != 2 {
		t.Errorf("desired/running = %d/%d, want 3/2", got.Desired, got.Running)
	}
	if got.State != "pending" {
		t.Errorf("state = %q, want pending for 2 of 3 replicas", got.State)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cluster/ -run TestRowsForServicesCarryDerivedState`
Expected: FAIL — `undefined: RowsForServices`

- [ ] **Step 3: Write the type and the service builder**

In `internal/cluster/view.go`:

```go
package cluster

import (
	"slices"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// Row is one entry in a list of cluster resources.
//
// It exists because the raw Docker object is the wrong answer to a list
// question: eight services of raw swarm.Service cost roughly fourteen thousand
// tokens, most of it Platforms entries and a duplicated PreviousSpec, and the
// field a caller actually asked for — whether the thing is healthy — is not in
// there at all, because state is derived from tasks.
//
// The shape is TopologyNode's, which already proved sufficient to diagnose a
// cluster in practice. Both ID and Name are always present so a caller never
// has to resolve one into the other, following the TargetID/TargetName pair the
// recommendations already use.
type Row struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Type is the resource type, singular: "service", "node", "task", ...
	Type string `json:"type"`

	// Stack is the owning stack namespace, where the resource belongs to one.
	Stack string `json:"stack,omitempty"`

	// State is the derived condition — a service's DeriveServiceState, a node's
	// status — not a raw Docker enum.
	State string `json:"state,omitempty"`

	// Detail is the single most identifying secondary fact: the image for a
	// service, the role for a node, the driver for a network.
	Detail string `json:"detail,omitempty"`

	// Desired and Running are populated only for types where a replica count
	// means something, so a caller can tell "2 of 3" from "no such concept".
	Desired int `json:"desired,omitempty"`
	Running int `json:"running,omitempty"`
}

// RowsForServices builds the list view of services. Tasks are needed because a
// service's state and running count are derived from them, not from its spec.
func RowsForServices(services []swarm.Service, tasks []swarm.Task) []Row {
	running := make(map[string]int, len(services))

	for _, task := range tasks {
		if task.Status.State == swarm.TaskStateRunning {
			running[task.ServiceID]++
		}
	}

	rows := make([]Row, 0, len(services))

	for _, svc := range services {
		var image string
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			image = StripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}

		rows = append(rows, Row{
			ID:      svc.ID,
			Name:    svc.Spec.Name,
			Type:    "service",
			Stack:   svc.Spec.Labels["com.docker.stack.namespace"],
			State:   DeriveServiceState(svc, running[svc.ID]),
			Detail:  image,
			Desired: ReplicaCount(svc),
			Running: running[svc.ID],
		})
	}

	sortRows(rows)

	return rows
}

// sortRows puts a list into a stable order. Callers build rows by ranging over
// cache slices whose order is not guaranteed, and the result is marshalled into
// an MCP result a client may cache by ETag.
func sortRows(rows []Row) {
	slices.SortFunc(rows, func(a, b Row) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cluster/ -run TestRowsForServicesCarryDerivedState`
Expected: PASS

- [ ] **Step 5: Write the failing test for the remaining seven types**

Append to `internal/cluster/view_test.go`:

```go
// Each builder must produce a row carrying an identity and a type, or find()
// has a hole for that resource. The assertion is deliberately shallow — the
// per-type detail is covered where it is interesting — and this exists to catch
// a type nobody wrote a builder for. Services are covered above; stacks and
// volumes are added in Steps 7 and 9 and get their cases then.
func TestBuildersProduceIdentifiableRows(t *testing.T) {
	cases := []struct {
		name     string
		rows     []Row
		wantType string
	}{
		{"nodes", RowsForNodes([]swarm.Node{{
			ID:          "n1",
			Description: swarm.NodeDescription{Hostname: "worker-a"},
			Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker},
			Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		}}), "node"},

		{"tasks", RowsForTasks(
			[]swarm.Task{{ID: "t1", ServiceID: "svc-api", NodeID: "n1",
				Status: swarm.TaskStatus{State: swarm.TaskStateRunning}}},
			[]swarm.Service{replicated("api", 1)},
			[]swarm.Node{{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-a"}}},
		), "task"},

		{"configs", RowsForConfigs([]swarm.Config{{
			ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "conf"}},
		}}), "config"},

		{"secrets", RowsForSecrets([]swarm.Secret{{
			ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}},
		}}), "secret"},

		{"networks", RowsForNetworks([]network.Summary{overlay("net1", "demo_overlay")}), "network"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(tc.rows))
			}

			got := tc.rows[0]

			if got.Type != tc.wantType {
				t.Errorf("type = %q, want %q", got.Type, tc.wantType)
			}
			if got.ID == "" || got.Name == "" {
				t.Errorf("row = %+v, want both an id and a name", got)
			}
		})
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/cluster/ -run TestBuildersProduceIdentifiableRows`
Expected: FAIL — `undefined: RowsForNodes`

- [ ] **Step 7: Write the remaining builders**

Append to `internal/cluster/view.go`:

```go
// RowsForNodes builds the list view of cluster nodes. Detail is the node's
// role, which is what distinguishes two otherwise identical ready nodes.
func RowsForNodes(nodes []swarm.Node) []Row {
	rows := make([]Row, 0, len(nodes))

	for _, n := range nodes {
		rows = append(rows, Row{
			ID:     n.ID,
			Name:   n.Description.Hostname,
			Type:   "node",
			State:  string(n.Status.State),
			Detail: string(n.Spec.Role),
		})
	}

	sortRows(rows)

	return rows
}

// RowsForTasks builds the list view of tasks. Services and nodes are needed to
// name the task's parents: a task's own record holds only their IDs, and a
// caller reading a task list is asking which service is broken and where.
func RowsForTasks(tasks []swarm.Task, services []swarm.Service, nodes []swarm.Node) []Row {
	serviceNames := make(map[string]string, len(services))
	for _, svc := range services {
		serviceNames[svc.ID] = svc.Spec.Name
	}

	nodeNames := make(map[string]string, len(nodes))
	for _, n := range nodes {
		nodeNames[n.ID] = n.Description.Hostname
	}

	rows := make([]Row, 0, len(tasks))

	for _, task := range tasks {
		name := serviceNames[task.ServiceID]
		if name == "" {
			name = task.ID
		}

		rows = append(rows, Row{
			ID:     task.ID,
			Name:   name,
			Type:   "task",
			Stack:  "",
			State:  string(task.Status.State),
			Detail: nodeNames[task.NodeID],
		})
	}

	sortRows(rows)

	return rows
}

// RowsForConfigs builds the list view of configs. Config data is base64 and
// never belongs in a list, so Detail stays empty.
func RowsForConfigs(configs []swarm.Config) []Row {
	rows := make([]Row, 0, len(configs))

	for _, cfg := range configs {
		rows = append(rows, Row{
			ID:    cfg.ID,
			Name:  cfg.Spec.Name,
			Type:  "config",
			Stack: cfg.Spec.Labels["com.docker.stack.namespace"],
		})
	}

	sortRows(rows)

	return rows
}

// RowsForSecrets builds the list view of secrets. A secret's data is zeroed
// before it reaches here and nothing about it is ever placed in Detail.
func RowsForSecrets(secrets []swarm.Secret) []Row {
	rows := make([]Row, 0, len(secrets))

	for _, sec := range secrets {
		rows = append(rows, Row{
			ID:    sec.ID,
			Name:  sec.Spec.Name,
			Type:  "secret",
			Stack: sec.Spec.Labels["com.docker.stack.namespace"],
		})
	}

	sortRows(rows)

	return rows
}

// RowsForNetworks builds the list view of networks.
func RowsForNetworks(networks []network.Summary) []Row {
	rows := make([]Row, 0, len(networks))

	for _, net := range networks {
		rows = append(rows, Row{
			ID:     net.ID,
			Name:   net.Name,
			Type:   "network",
			Stack:  net.Labels["com.docker.stack.namespace"],
			Detail: net.Driver,
		})
	}

	sortRows(rows)

	return rows
}

// RowsForVolumes builds the list view of volumes. Volumes are keyed by Name
// rather than ID everywhere in Cetacean, so both fields carry the name.
func RowsForVolumes(volumes []*volume.Volume) []Row {
	rows := make([]Row, 0, len(volumes))

	for _, vol := range volumes {
		if vol == nil {
			continue
		}

		rows = append(rows, Row{
			ID:     vol.Name,
			Name:   vol.Name,
			Type:   "volume",
			Stack:  vol.Labels["com.docker.stack.namespace"],
			Detail: vol.Driver,
		})
	}

	sortRows(rows)

	return rows
}
```

- [ ] **Step 8: Run the full package test**

Run: `go test ./internal/cluster/`
Expected: PASS, all tests

- [ ] **Step 9: Add the stacks builder**

`Stack` is Cetacean's derived type, not a Docker one: `cache.Stack{Name string; Services, Configs, Secrets, Networks, Volumes []string}`. `internal/cluster` already imports `internal/cache` (see `search.go:10`), so there is no cycle to work around. Append to `internal/cluster/view.go`:

```go
// RowsForStacks builds the list view of stacks. A stack is derived from labels
// rather than being a Docker primitive, so its Detail is the thing that makes
// it worth listing: how many services it holds.
func RowsForStacks(stacks []cache.Stack) []Row {
	rows := make([]Row, 0, len(stacks))

	for _, st := range stacks {
		rows = append(rows, Row{
			ID:      st.Name,
			Name:    st.Name,
			Type:    "stack",
			Stack:   st.Name,
			Desired: len(st.Services),
		})
	}

	sortRows(rows)

	return rows
}
```

- [ ] **Step 10: Run tests and commit**

Run: `go test ./internal/cluster/ && make lint && make fmt-check`
Expected: PASS, 0 lint issues

```bash
git add internal/cluster/view.go internal/cluster/view_test.go
git commit -m "feat(cluster): add the compact Row shape for resource lists"
```

---

### Task 2: The `Digest` envelope and its `reason`

**Files:**
- Modify: `internal/cluster/view.go`
- Modify: `internal/cluster/view_test.go`

**Interfaces:**
- Consumes: `cluster.Row` from Task 1.
- Produces: `cluster.Digest` struct; `cluster.Related` struct; `cluster.ServiceDigest(swarm.Service, []swarm.Task, []swarm.Node) Digest`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cluster/view_test.go`:

```go
// The reason is the point of a digest. "state: failed" is not an answer; the
// unmet constraint is. Swarm puts it in Status.Err, which is where this reads.
func TestServiceDigestExplainsWhyAServiceIsNotRunning(t *testing.T) {
	svc := replicated("stuck", 1)
	svc.Spec.TaskTemplate.Placement = &swarm.Placement{
		Constraints: []string{"node.labels.gpu == true"},
	}

	tasks := []swarm.Task{{
		ID:           "t1",
		ServiceID:    "svc-stuck",
		DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{
			State:   swarm.TaskStatePending,
			Message: "pending task scheduling",
			Err:     "no suitable node (scheduling constraints not satisfied on 1 node)",
		},
	}}

	got := ServiceDigest(svc, tasks, nil)

	if got.Reason != "no suitable node (scheduling constraints not satisfied on 1 node)" {
		t.Errorf("reason = %q, want Status.Err verbatim", got.Reason)
	}
	if got.State == "running" {
		t.Errorf("state = %q, want a non-running state", got.State)
	}
	if len(got.RecentFailures) != 1 {
		t.Fatalf("recentFailures = %d, want the pending task", len(got.RecentFailures))
	}
}

// A healthy service has nothing to explain, and a reason on a healthy resource
// would read as though something were wrong.
func TestServiceDigestOmitsReasonWhenHealthy(t *testing.T) {
	svc := replicated("api", 1)
	tasks := []swarm.Task{{
		ID: "t1", ServiceID: "svc-api", DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
	}}

	got := ServiceDigest(svc, tasks, nil)

	if got.Reason != "" {
		t.Errorf("reason = %q, want empty for a converged service", got.Reason)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cluster/ -run TestServiceDigest`
Expected: FAIL — `undefined: ServiceDigest`

- [ ] **Step 3: Write the Digest type and the service builder**

Append to `internal/cluster/view.go`:

```go
// Digest is the detail view of one resource: everything a caller needs to
// decide what to do, and nothing they would have to ask a second question for.
//
// Reason is the field that earns the type. A state alone — "failed" — is not an
// answer to "why is this broken", so every read that reports a non-healthy
// state also reports the cause Swarm gave for it, and a follow-up call is not
// required to learn it.
type Digest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`

	State string `json:"state,omitempty"`

	// Reason explains a non-healthy State, and is empty when there is nothing
	// to explain.
	Reason string `json:"reason,omitempty"`

	// Details is type-specific. It is deliberately open in the advertised
	// output schema: eight describe_<type> tools would tighten it at the cost
	// of eight entries in every tools/list, and the envelope is where the
	// tightness matters.
	Details map[string]any `json:"details,omitempty"`

	// Related are the resources this one references or is referenced by, so a
	// caller can traverse without a second search.
	Related []Related `json:"related"`

	// RecentFailures are the task failures behind the current State, newest
	// first, capped at maxRecentFailures.
	RecentFailures []TaskFailure `json:"recentFailures"`
}

// Related is one cross-reference from a Digest.
type Related struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`

	// Relation names the direction, e.g. "attached-to", "mounts", "runs-on".
	Relation string `json:"relation"`
}

// TaskFailure is one task that did not run, with the reason it did not.
type TaskFailure struct {
	TaskID  string `json:"taskId"`
	At      string `json:"at,omitempty"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// maxRecentFailures bounds what a digest carries. A service restarting in a
// loop can hold dozens of failed records and they all say the same thing.
const maxRecentFailures = 5

// ServiceDigest builds the detail view of one service.
func ServiceDigest(svc swarm.Service, tasks []swarm.Task, nodes []swarm.Node) Digest {
	nodeNames := make(map[string]string, len(nodes))
	for _, n := range nodes {
		nodeNames[n.ID] = n.Description.Hostname
	}

	var (
		running  int
		failures []TaskFailure
	)

	for _, task := range tasks {
		if task.ServiceID != svc.ID {
			continue
		}

		if task.Status.State == swarm.TaskStateRunning {
			running++

			continue
		}

		// A task the orchestrator has already replaced explains history, not
		// the current state, and including it would attribute an old failure
		// to a service that has since recovered.
		if task.DesiredState == swarm.TaskStateShutdown ||
			task.DesiredState == swarm.TaskStateRemove {
			continue
		}

		failures = append(failures, TaskFailure{
			TaskID:  task.ID,
			At:      task.Status.Timestamp.UTC().Format(time.RFC3339),
			State:   string(task.Status.State),
			Message: taskReason(task),
		})
	}

	slices.SortFunc(failures, func(a, b TaskFailure) int {
		return strings.Compare(b.At, a.At)
	})

	if len(failures) > maxRecentFailures {
		failures = failures[:maxRecentFailures]
	}

	state := DeriveServiceState(svc, running)

	digest := Digest{
		ID:             svc.ID,
		Name:           svc.Spec.Name,
		Type:           "service",
		State:          state,
		Related:        []Related{},
		RecentFailures: failures,
	}

	if state != "running" && len(failures) > 0 {
		digest.Reason = failures[0].Message
	}

	return digest
}

// taskReason is the cause Swarm gave for a task not running. Status.Err holds
// the actionable text ("no suitable node (scheduling constraints not satisfied
// on 1 node)"); Status.Message is the lifecycle narration ("pending task
// scheduling") and is only worth returning when there is no error.
func taskReason(task swarm.Task) string {
	if task.Status.Err != "" {
		return task.Status.Err
	}

	return task.Status.Message
}
```

Add `"time"` to the import block.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cluster/ -run TestServiceDigest`
Expected: PASS both tests

- [ ] **Step 5: Commit**

```bash
git add internal/cluster/view.go internal/cluster/view_test.go
git commit -m "feat(cluster): add the Digest shape, carrying why a resource is unhealthy"
```

---

### Task 3: Service digest details, in named units

**Files:**
- Modify: `internal/cluster/view.go`
- Modify: `internal/cluster/view_test.go`

**Interfaces:**
- Consumes: `cluster.Digest` from Task 2.
- Produces: populated `Digest.Details` for services; `cluster.ServiceDetails(swarm.Service) map[string]any`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cluster/view_test.go`:

```go
// Units are named because the alternative shipped a bug: a recommendation once
// reported "configured 25, suggested 50000000" for the same quantity. A field
// name that carries its unit cannot be misread.
func TestServiceDetailsNameTheirUnits(t *testing.T) {
	svc := replicated("api", 2)
	svc.Spec.TaskTemplate.Resources = &swarm.ResourceRequirements{
		Limits:       &swarm.Limit{NanoCPUs: 2e9, MemoryBytes: 1 << 30},
		Reservations: &swarm.Resources{NanoCPUs: 5e8, MemoryBytes: 256 << 20},
	}
	svc.Spec.TaskTemplate.ContainerSpec.Healthcheck = &container.HealthConfig{
		Test:     []string{"CMD", "true"},
		Interval: 10 * time.Second,
	}
	svc.Spec.TaskTemplate.ContainerSpec.Env = []string{"DB_PASSWORD=hunter2", "PORT=8080"}

	got := ServiceDetails(svc)

	if got["cpuLimitCores"] != 2.0 {
		t.Errorf("cpuLimitCores = %v, want 2", got["cpuLimitCores"])
	}
	if got["memoryLimitBytes"] != int64(1<<30) {
		t.Errorf("memoryLimitBytes = %v, want 1073741824", got["memoryLimitBytes"])
	}
	if got["healthcheckInterval"] != "10s" {
		t.Errorf("healthcheckInterval = %v, want \"10s\"", got["healthcheckInterval"])
	}

	// Env names only: env is where credentials live.
	names, ok := got["envNames"].([]string)
	if !ok {
		t.Fatalf("envNames = %T, want []string", got["envNames"])
	}
	if !slices.Equal(names, []string{"DB_PASSWORD", "PORT"}) {
		t.Errorf("envNames = %v, want the names sorted", names)
	}

	for key, value := range got {
		if str, isStr := value.(string); isStr && strings.Contains(str, "hunter2") {
			t.Fatalf("details[%q] leaked an env value", key)
		}
	}
}
```

Add `"github.com/docker/docker/api/types/container"`, `"slices"`, `"strings"` and `"time"` to the test imports.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cluster/ -run TestServiceDetailsNameTheirUnits`
Expected: FAIL — `undefined: ServiceDetails`

- [ ] **Step 3: Implement**

Append to `internal/cluster/view.go`:

```go
// ServiceDetails is the type-specific body of a service's Digest.
//
// Every numeric field names its unit. Docker's own types express CPU in
// NanoCPUs and durations in nanoseconds, which read as unlabelled large
// integers and have already caused one shipped bug; a caller should not have to
// know that 10000000000 is ten seconds.
func ServiceDetails(svc swarm.Service) map[string]any {
	details := map[string]any{
		"mode":     serviceMode(svc),
		"replicas": ReplicaCount(svc),
	}

	spec := svc.Spec.TaskTemplate.ContainerSpec
	if spec != nil {
		details["image"] = StripImageDigest(spec.Image)

		if len(spec.Args) > 0 {
			details["command"] = spec.Args
		}

		names := make([]string, 0, len(spec.Env))
		for _, entry := range spec.Env {
			name, _, _ := strings.Cut(entry, "=")
			names = append(names, name)
		}

		slices.Sort(names)
		details["envNames"] = names

		if hc := spec.Healthcheck; hc != nil {
			details["healthcheck"] = true

			if hc.Interval > 0 {
				details["healthcheckInterval"] = hc.Interval.String()
			}
		} else {
			details["healthcheck"] = false
		}
	}

	if res := svc.Spec.TaskTemplate.Resources; res != nil {
		if res.Limits != nil {
			if res.Limits.NanoCPUs > 0 {
				details["cpuLimitCores"] = float64(res.Limits.NanoCPUs) / 1e9
			}

			if res.Limits.MemoryBytes > 0 {
				details["memoryLimitBytes"] = res.Limits.MemoryBytes
			}
		}

		if res.Reservations != nil {
			if res.Reservations.NanoCPUs > 0 {
				details["cpuReservationCores"] = float64(res.Reservations.NanoCPUs) / 1e9
			}

			if res.Reservations.MemoryBytes > 0 {
				details["memoryReservationBytes"] = res.Reservations.MemoryBytes
			}
		}
	}

	if p := svc.Spec.TaskTemplate.Placement; p != nil && len(p.Constraints) > 0 {
		details["placementConstraints"] = p.Constraints
	}

	if svc.UpdateStatus != nil && svc.UpdateStatus.State != "" {
		details["updateState"] = string(svc.UpdateStatus.State)

		if svc.UpdateStatus.Message != "" {
			details["updateMessage"] = svc.UpdateStatus.Message
		}
	}

	return details
}

// serviceMode names a service's scheduling mode without exposing the nested
// Docker union a caller would otherwise have to interpret.
func serviceMode(svc swarm.Service) string {
	if svc.Spec.Mode.Global != nil {
		return "global"
	}

	return "replicated"
}
```

- [ ] **Step 4: Wire details into `ServiceDigest`**

In `ServiceDigest`, before the `return`:

```go
	digest.Details = ServiceDetails(svc)
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/cluster/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cluster/view.go internal/cluster/view_test.go
git commit -m "feat(cluster): describe a service in named units, env names only"
```

---

### Task 4: The `find` tool

**Files:**
- Create: `internal/mcp/find.go`
- Create: `internal/mcp/find_test.go`
- Delete: `internal/mcp/list_resources.go`, `internal/mcp/list_resources_test.go`
- Modify: `internal/mcp/tools.go` — replace the `list_resources` and `search` registrations

**Interfaces:**
- Consumes: `cluster.RowsFor*` from Task 1.
- Produces: `findResult` struct (`{type, items []cluster.Row, total int}`); tool `find`.

- [ ] **Step 1: Write the failing test**

In `internal/mcp/find_test.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// find returns rows, not Docker objects: the whole point is the size of the
// answer, and a raw swarm.Service costs roughly 1.7k tokens each.
func TestFindReturnsCompactRows(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "demo_web",
				Labels: map[string]string{"com.docker.stack.namespace": "demo"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:alpine@sha256:abc"},
			},
		},
	})

	srv := newLogTestServer(t, c, &fakeLogStreamer{})

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	out, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"type": "services",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var got findResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}

	if got.Total != 1 || len(got.Items) != 1 {
		t.Fatalf("total/items = %d/%d, want 1/1", got.Total, len(got.Items))
	}

	row := got.Items[0]

	if row.Name != "demo_web" || row.Stack != "demo" || row.Detail != "nginx:alpine" {
		t.Errorf("row = %+v, want the compact shape with a digest-free image", row)
	}

	// The raw Docker keys must not be in the payload at all.
	if bytes := []byte(out); json.Valid(bytes) {
		var probe map[string]any
		_ = json.Unmarshal(bytes, &probe)

		if _, leaked := probe["Spec"]; leaked {
			t.Error("payload carries raw Docker fields")
		}
	}
}

// A type nobody wrote a builder for must fail loudly rather than return an
// empty list that reads as "no such resources".
func TestFindRejectsUnknownType(t *testing.T) {
	srv := newLogTestServer(t, cache.New(nil), &fakeLogStreamer{})
	td, _ := srv.findTool("find")

	_, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"type": "widgets",
	}))
	if err == nil {
		t.Fatal("expected an error for an unknown type")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/mcp/ -run TestFind`
Expected: FAIL — `find not registered`

- [ ] **Step 3: Implement `find.go`**

Create `internal/mcp/find.go` modelled on the deleted `list_resources.go`: keep `listableResourceTypes`, `defaultListLimit`, `toSlice` and the `lookupResource` delegation exactly as they are — that delegation is what keeps ACL filtering, secret redaction and task enrichment on one audited path — and convert the resulting slice to `[]cluster.Row` with a type switch that calls the Task 1 builders. Add the optional filters (`query`, `state`, `stack`, `node`, `image`, `label`) as post-filters over the rows, and `raw: true` to return the untouched slice instead.

```go
// findResult is the envelope for a list of resources.
//
// Total is the count before paging and after filtering, so a caller can say
// "showing 200 of 1,432" without a second call.
type findResult struct {
	Type  string        `json:"type"`
	Items []cluster.Row `json:"items"`
	Total int           `json:"total"`
}
```

- [ ] **Step 4: Register the tool in `tools.go`**

Replace the `list_resources` entry. Keep `tier: config.OpsReadOnly`, all four behaviour hints, and `mcplib.WithOutputSchema[findResult]()`. Remove the `search` entry — `find` with a `query` and no `type` subsumes it.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/mcp/ -run TestFind`
Expected: PASS

- [ ] **Step 6: Fix the fallout**

Run: `go build ./... && go test ./...`
Expected: failures in tests referencing `list_resources` or `search`. Update each to `find`. `TestListResourcesCoversEveryListableType` moves to `find_test.go` unchanged in intent.

- [ ] **Step 7: Verify the schema matches the result**

This is the test the `search` bug needed. Add to `find_test.go`:

```go
// The search tool once advertised a schema requiring Hits while emitting
// results, and no test compared the two. Every tool with an output schema gets
// this check.
func TestFindResultMatchesItsOutputSchema(t *testing.T) {
	srv := newLogTestServer(t, cache.New(nil), &fakeLogStreamer{})

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	schema, err := json.Marshal(td.tool.OutputSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}

	var parsed struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	out, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"type": "services",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	for _, key := range parsed.Required {
		if _, present := payload[key]; !present {
			t.Errorf("schema requires %q, result does not carry it", key)
		}
	}
}
```

- [ ] **Step 8: Run everything and commit**

Run: `go test ./... && make lint && make fmt-check`

```bash
git add -A
git commit -m "feat(mcp): replace list_resources and search with find over compact rows"
```

---

### Task 5: The `describe` tool and the resource payloads

**Files:**
- Create: `internal/mcp/describe.go`, `internal/mcp/describe_test.go`
- Modify: `internal/mcp/resources.go` — templated reads return `cluster.Digest`
- Modify: `internal/mcp/tools.go` — register `describe`

**Interfaces:**
- Consumes: `cluster.ServiceDigest` from Tasks 2–3.
- Produces: tool `describe`.

- [ ] **Step 1: Write the failing test**

In `internal/mcp/describe_test.go`, assert that `describe` on the unschedulable service returns the reason, and that reading `cetacean://services/{id}` returns the same digest — one builder, two transports:

```go
func TestDescribeAndResourceReadAgree(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "demo_stuck"},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "nginx:alpine"}},
		},
	})
	c.SetTask(swarm.Task{
		ID: "t1", ServiceID: "svc1", DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{
			State: swarm.TaskStatePending,
			Err:   "no suitable node (scheduling constraints not satisfied on 1 node)",
		},
	})

	srv := newLogTestServer(t, c, &fakeLogStreamer{})

	td, ok := srv.findTool("describe")
	if !ok {
		t.Fatal("describe not registered")
	}

	out, err := td.handler(context.Background(), newCallToolRequest("describe", map[string]any{
		"type": "service",
		"id":   "svc1",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var got cluster.Digest
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}

	if got.Reason == "" {
		t.Error("digest carries no reason for a service that cannot schedule")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/mcp/ -run TestDescribeAndResourceReadAgree`
Expected: FAIL — `describe not registered`

- [ ] **Step 3: Implement `describe.go`**

Resolve `type` against a singular-name map, delegate identity resolution and ACL to `lookupResource` exactly as `find` does, then build the digest. Accept `raw: true` for the untouched Docker object.

- [ ] **Step 4: Change the templated resource payloads**

In `resources.go`, the eight templated handlers return `cluster.Digest` built by the same functions. **Do not remove the resources or their URIs** — they carry subscriptions via `NotificationManager`, and a tool cannot.

- [ ] **Step 5: Register `describe` in `tools.go`**

`tier: config.OpsReadOnly`, four behaviour hints, `mcplib.WithOutputSchema[cluster.Digest]()`.

- [ ] **Step 6: Run and commit**

Run: `go test ./... && make lint && make fmt-check`

```bash
git add -A
git commit -m "feat(mcp): add describe, and serve the digest from the resource reads too"
```

---

### Task 6: Migrate the table widget

**Files:**
- Modify: `frontend/src/widgets/table/columns.ts`
- Modify: `frontend/src/widgets/table/columns.test.ts`
- Modify: `frontend/src/widgets/table/TableWidget.tsx` (tool name `list_resources` → `find`)

**Interfaces:**
- Consumes: the `findResult` wire shape from Task 4.
- Produces: no Go interface.

- [ ] **Step 1: Write the failing test**

Rewrite `columns.test.ts` to assert against rows, not Docker objects:

```ts
import { describe, expect, it } from "vitest";

import { columnsFor } from "./columns";

describe("columnsFor", () => {
  it("reads a compact row without per-type accessors", () => {
    const row = {
      id: "svc1",
      name: "demo_web",
      type: "service",
      stack: "demo",
      state: "running",
      detail: "nginx:alpine",
      desired: 2,
      running: 2,
    };

    const rendered = columnsFor("services").map(({ key, value }) => [key, value(row)]);

    expect(Object.fromEntries(rendered)).toMatchObject({
      name: "demo_web",
      state: "running",
      detail: "nginx:alpine",
    });
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && npx vitest run src/widgets/table/columns.test.ts`
Expected: FAIL — accessors read `Spec.Name`

- [ ] **Step 3: Rewrite `columns.ts`**

Every row carries `id`, `name`, `state` and `detail` under those names, so the per-type accessor table and the unknown-type fallback both go. What remains is which columns each type *shows* and what each header is called — for example `desired`/`running` only for services and tasks. Delete `path()` and `text()`; they existed only to walk nested Docker objects.

- [ ] **Step 4: Run to verify it passes**

Run: `cd frontend && npx vitest run src/widgets/table/ && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 5: Rebuild the widgets and commit**

Run: `cd frontend && npm run build:widgets`

```bash
git add frontend/src/widgets/table
git commit -m "feat(widgets): render compact rows, dropping the Docker field accessors"
```

---

### Task 7: Documentation and changelog

**Files:**
- Modify: `docs/mcp.md`
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md` — the `mcp/` and `cluster/` bullets

- [ ] **Step 1: Update `docs/mcp.md`**

Replace the `list_resources` and `search` references with `find`; document `describe`, the row and digest shapes, the named-unit rule, and `raw: true` as the documented-expensive escape hatch.

- [ ] **Step 2: Update `CHANGELOG.md`**

Under `[Unreleased] / Changed`, user-facing and concise, marked breaking as the `search` rename already is:

```markdown
- **Breaking:** AI agents now receive a compact description of cluster resources rather than raw Docker output — a service comes back as its name, stack, image, state and replica counts instead of several hundred lines of engine detail, so an agent can read a whole cluster for what one service used to cost. Listing and searching are now one `find` tool, and a new `describe` tool answers what a resource is and, when something is wrong with it, why. Agents needing the raw Docker object can still ask for it
```

- [ ] **Step 3: Update `CLAUDE.md`**

The `internal/cluster/` bullet gains `Row`/`Digest`; the `internal/mcp/` bullet's tool count and the `list_resources.go` entry change to `find.go`/`describe.go`.

- [ ] **Step 4: Verify and commit**

Run: `make check`

```bash
git add docs/mcp.md CHANGELOG.md CLAUDE.md
git commit -m "docs: describe the compact MCP representation"
```

---

### Task 8: Verify against a live cluster

Not optional. Every defect this design responds to was found by driving the tools against a real cluster and none by reading the code.

- [ ] **Step 1: Build and deploy**

```bash
make build && docker build -t cetacean:latest . && docker service update --force --image cetacean:latest cetacean_cetacean
```

- [ ] **Step 2: Measure the payload**

Call `find` with `type: "services"` and compare its size against the ~14k tokens `list_resources` cost for the same 8 services. Record the number in the PR description; the target is an order of magnitude.

- [ ] **Step 3: Check the reason field**

Call `describe` with `type: "service"`, `id: "demo_stuck"`. Expect `reason` to read `no suitable node (scheduling constraints not satisfied on 1 node)`. A state without a reason here means Task 2 regressed.

- [ ] **Step 4: Check the widget**

Open the table widget in an MCP Apps host and confirm the rows render, including for a type `columns.ts` has no explicit entry for.
