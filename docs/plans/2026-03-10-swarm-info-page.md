# Swarm Info Page Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `/swarm` page showing Docker Swarm cluster metadata (cluster ID, join tokens, raft/CA/orchestration config, encryption settings).

**Architecture:** New `SwarmInspect()` method on docker client → new `GET /api/swarm` handler (calls Docker directly, no cache) → new `SwarmPage` React component. Join tokens hidden behind reveal toggle.

**Tech Stack:** Go (Docker SDK `swarm.Swarm`), React 19, TypeScript, Tailwind CSS, existing InfoCard/PageHeader components.

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/docker/client.go` | Add `SwarmInspect()` method |
| Modify | `internal/api/handlers.go` | Add `SwarmInspector` interface, `HandleSwarm` handler, wire into `Handlers` struct |
| Modify | `internal/api/router.go` | Register `GET /api/swarm` route |
| Modify | `main.go` | Pass docker client as `SwarmInspector` to `NewHandlers` |
| Modify | `frontend/src/api/types.ts` | Add `SwarmInfo` TypeScript interface |
| Modify | `frontend/src/api/client.ts` | Add `api.swarm()` method |
| Create | `frontend/src/pages/SwarmPage.tsx` | Swarm info page component |
| Modify | `frontend/src/App.tsx` | Add route and nav link |

---

## Chunk 1: Backend

### Task 1: Add SwarmInspect to Docker Client

**Files:**
- Modify: `internal/docker/client.go`

- [ ] **Step 1: Add SwarmInspect method**

Add after the `InspectVolume` method (line 132):

```go
func (c *Client) SwarmInspect(ctx context.Context) (swarm.Swarm, error) {
	return c.docker.SwarmInspect(ctx)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/docker/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/docker/client.go
git commit -m "feat: add SwarmInspect method to docker client"
```

### Task 2: Add SwarmInspector Interface and Handler

**Files:**
- Modify: `internal/api/handlers.go`

- [ ] **Step 1: Add SwarmInspector interface**

Add below the `DockerLogStreamer` interface (after line 35):

```go
type SwarmInspector interface {
	SwarmInspect(ctx context.Context) (swarm.Swarm, error)
}
```

- [ ] **Step 2: Add swarmClient field to Handlers struct**

Change the `Handlers` struct to add a `swarmClient` field:

```go
type Handlers struct {
	cache        *cache.Cache
	dockerClient DockerLogStreamer
	swarmClient  SwarmInspector
	ready        <-chan struct{}
	notifier     *notify.Notifier
	promClient   *PromClient
}
```

- [ ] **Step 3: Update NewHandlers to accept SwarmInspector**

```go
func NewHandlers(c *cache.Cache, dc DockerLogStreamer, sc SwarmInspector, ready <-chan struct{}, notifier *notify.Notifier, promClient *PromClient) *Handlers {
	return &Handlers{cache: c, dockerClient: dc, swarmClient: sc, ready: ready, notifier: notifier, promClient: promClient}
}
```

- [ ] **Step 4: Add HandleSwarm handler**

Add at end of file (before any helper functions, after the last handler):

```go
func (h *Handlers) HandleSwarm(w http.ResponseWriter, r *http.Request) {
	if h.swarmClient == nil {
		writeError(w, http.StatusNotImplemented, "swarm inspect not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	sw, err := h.swarmClient.SwarmInspect(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("swarm inspect failed: %v", err))
		return
	}

	// Find manager address from cache for join command display
	managerAddr := ""
	for _, n := range h.cache.Nodes() {
		if n.ManagerStatus != nil && n.ManagerStatus.Leader {
			managerAddr = n.ManagerStatus.Addr
			break
		}
	}

	writeJSON(w, struct {
		Swarm       swarm.Swarm `json:"swarm"`
		ManagerAddr string      `json:"managerAddr"`
	}{sw, managerAddr})
}
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/api/`
Expected: compilation error in main.go (NewHandlers signature changed) — that's expected, we fix it next.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go
git commit -m "feat: add SwarmInspector interface and HandleSwarm handler"
```

### Task 3: Wire Up Router and Main

**Files:**
- Modify: `internal/api/router.go`
- Modify: `main.go`

- [ ] **Step 1: Add route to router.go**

Add after the `GET /api/cluster/metrics` line (after line 20):

```go
	mux.HandleFunc("GET /api/swarm", h.HandleSwarm)
```

- [ ] **Step 2: Update main.go NewHandlers call**

Change line 110 from:
```go
handlers := api.NewHandlers(stateCache, dockerClient, watcher.Ready(), notifier, promClient)
```
to:
```go
handlers := api.NewHandlers(stateCache, dockerClient, dockerClient, watcher.Ready(), notifier, promClient)
```

(The docker client satisfies both `DockerLogStreamer` and `SwarmInspector`.)

- [ ] **Step 3: Verify full build compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build .`
Expected: no errors

- [ ] **Step 4: Run Go tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./...`
Expected: all pass (existing tests may need `NewHandlers` call updated if any exist)

- [ ] **Step 5: Commit**

```bash
git add internal/api/router.go main.go
git commit -m "feat: register GET /api/swarm route and wire SwarmInspector"
```

---

## Chunk 2: Frontend

### Task 4: Add TypeScript Types and API Method

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add SwarmInfo interface to types.ts**

Add at end of file:

```typescript
export interface SwarmInfo {
  swarm: {
    ID: string;
    CreatedAt: string;
    UpdatedAt: string;
    Spec: {
      Annotations: { Name: string; Labels: Record<string, string> };
      Orchestration: { TaskHistoryRetentionLimit?: number };
      Raft: {
        SnapshotInterval: number;
        KeepOldSnapshots?: number;
        LogEntriesForSlowFollowers: number;
        ElectionTick: number;
        HeartbeatTick: number;
      };
      Dispatcher: { HeartbeatPeriod: number };
      CAConfig: {
        NodeCertExpiry: number;
        ExternalCAs?: Array<{ Protocol: string; URL: string; Options?: Record<string, string> }>;
        ForceRotate: number;
      };
      TaskDefaults: {
        LogDriver?: { Name: string; Options?: Record<string, string> };
      };
      EncryptionConfig: { AutoLockManagers: boolean };
    };
    TLSInfo: {
      TrustRoot: string;
      CertIssuerSubject: string;
      CertIssuerPublicKey: string;
    };
    RootRotationInProgress: boolean;
    DefaultAddrPool: string[];
    SubnetSize: number;
    DataPathPort: number;
    JoinTokens: { Worker: string; Manager: string };
  };
  managerAddr: string;
}
```

- [ ] **Step 2: Add api.swarm() to client.ts**

Add to the imports at top of client.ts:
```typescript
  SwarmInfo,
```

Add to the `api` object:
```typescript
  swarm: () => fetchJSON<SwarmInfo>("/swarm"),
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat: add SwarmInfo type and api.swarm() client method"
```

### Task 5: Create SwarmPage Component

**Files:**
- Create: `frontend/src/pages/SwarmPage.tsx`

- [ ] **Step 1: Create SwarmPage.tsx**

```tsx
import { useCallback, useEffect, useState } from "react";
import { api } from "../api/client";
import type { SwarmInfo } from "../api/types";
import { ResourceId, Timestamp } from "../components/data";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { LoadingDetail } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";

function formatNs(ns: number): string {
  if (ns <= 0) return "—";
  const ms = ns / 1_000_000;
  if (ms < 1000) return `${ms}ms`;
  const sec = ms / 1000;
  if (sec < 60) return `${sec}s`;
  const min = sec / 60;
  if (min < 60) return `${Math.round(min)}m`;
  const hrs = min / 60;
  if (hrs < 24) return `${Math.round(hrs)}h`;
  const days = hrs / 24;
  return `${Math.round(days)}d`;
}

function JoinToken({
  label,
  token,
  managerAddr,
}: {
  label: string;
  token: string;
  managerAddr: string;
}) {
  const [revealed, setRevealed] = useState(false);
  const joinCmd = managerAddr
    ? `docker swarm join --token ${token} ${managerAddr}`
    : token;

  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-sm font-medium text-muted-foreground">{label}</span>
      {revealed ? (
        <div className="flex items-start gap-2">
          <code className="flex-1 text-xs font-mono bg-muted rounded p-2 break-all select-all">
            {joinCmd}
          </code>
          <button
            type="button"
            onClick={() => setRevealed(false)}
            className="shrink-0 text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
          >
            Hide
          </button>
        </div>
      ) : (
        <div className="flex items-center gap-2">
          <span className="text-sm text-muted-foreground font-mono">••••••••••••••••</span>
          <button
            type="button"
            onClick={() => setRevealed(true)}
            className="text-xs text-link hover:underline cursor-pointer"
          >
            Reveal
          </button>
        </div>
      )}
    </div>
  );
}

function KVTable({
  rows,
}: {
  rows: (false | undefined | null | 0 | "" | [string, React.ReactNode])[];
}) {
  const valid = rows.filter(
    (row): row is [string, React.ReactNode] => !!row && !!row[1],
  );
  if (valid.length === 0) return null;
  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full">
        <tbody>
          {valid.map(([k, v]) => (
            <tr key={k} className="border-b last:border-b-0">
              <td className="p-3 text-sm font-medium text-muted-foreground w-1/3">
                {k}
              </td>
              <td className="p-3 font-mono text-xs break-all">{v}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function SectionHeader({ title }: { title: string }) {
  return (
    <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
      {title}
    </h2>
  );
}

export default function SwarmPage() {
  const [data, setData] = useState<SwarmInfo | null>(null);
  const [error, setError] = useState(false);

  const fetchData = useCallback(() => {
    api.swarm().then(setData).catch(() => setError(true));
  }, []);

  useEffect(fetchData, [fetchData]);

  if (error) return <FetchError message="Failed to load swarm info" />;
  if (!data) return <LoadingDetail />;

  const { swarm: sw, managerAddr } = data;
  const spec = sw.Spec;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Swarm" />

      {/* Overview */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <ResourceId label="Cluster ID" id={sw.ID} />
        <Timestamp label="Created" date={sw.CreatedAt} />
        <Timestamp label="Updated" date={sw.UpdatedAt} />
        <InfoCard
          label="Default Address Pool"
          value={sw.DefaultAddrPool?.join(", ") || "—"}
        />
        <InfoCard
          label="Subnet Size"
          value={sw.SubnetSize ? `/${sw.SubnetSize}` : "—"}
        />
        <InfoCard
          label="Data Path Port"
          value={sw.DataPathPort ? String(sw.DataPathPort) : "—"}
        />
      </div>

      {/* Join Tokens */}
      <div>
        <SectionHeader title="Join Tokens" />
        <div className="rounded-lg border p-4 flex flex-col gap-4">
          <JoinToken
            label="Worker"
            token={sw.JoinTokens.Worker}
            managerAddr={managerAddr}
          />
          <JoinToken
            label="Manager"
            token={sw.JoinTokens.Manager}
            managerAddr={managerAddr}
          />
        </div>
      </div>

      {/* Orchestration */}
      <div>
        <SectionHeader title="Orchestration" />
        <KVTable
          rows={[
            [
              "Task History Retention Limit",
              spec.Orchestration.TaskHistoryRetentionLimit != null
                ? String(spec.Orchestration.TaskHistoryRetentionLimit)
                : "—",
            ],
          ]}
        />
      </div>

      {/* Raft */}
      <div>
        <SectionHeader title="Raft" />
        <KVTable
          rows={[
            ["Snapshot Interval", String(spec.Raft.SnapshotInterval)],
            spec.Raft.KeepOldSnapshots != null && [
              "Keep Old Snapshots",
              String(spec.Raft.KeepOldSnapshots),
            ],
            [
              "Log Entries for Slow Followers",
              String(spec.Raft.LogEntriesForSlowFollowers),
            ],
            ["Election Tick", `${spec.Raft.ElectionTick} ticks`],
            ["Heartbeat Tick", `${spec.Raft.HeartbeatTick} ticks`],
          ]}
        />
      </div>

      {/* CA Configuration */}
      <div>
        <SectionHeader title="CA Configuration" />
        <KVTable
          rows={[
            spec.CAConfig.NodeCertExpiry !== 0 && [
              "Node Certificate Expiry",
              formatNs(spec.CAConfig.NodeCertExpiry),
            ],
            [
              "Force Rotate",
              String(spec.CAConfig.ForceRotate),
            ],
            [
              "Root Rotation In Progress",
              sw.RootRotationInProgress ? "Yes" : "No",
            ],
            ...(spec.CAConfig.ExternalCAs?.map(
              (ca, i): [string, string] => [
                `External CA ${i + 1}`,
                `${ca.Protocol} — ${ca.URL}`,
              ],
            ) ?? []),
          ]}
        />
      </div>

      {/* Dispatcher */}
      <div>
        <SectionHeader title="Dispatcher" />
        <KVTable
          rows={[
            spec.Dispatcher.HeartbeatPeriod !== 0 && [
              "Heartbeat Period",
              formatNs(spec.Dispatcher.HeartbeatPeriod),
            ],
          ]}
        />
      </div>

      {/* Encryption */}
      <div>
        <SectionHeader title="Encryption" />
        <KVTable
          rows={[
            [
              "Auto-Lock Managers",
              spec.EncryptionConfig.AutoLockManagers ? "Yes" : "No",
            ],
          ]}
        />
      </div>

      {/* Task Defaults */}
      {spec.TaskDefaults.LogDriver && (
        <div>
          <SectionHeader title="Task Defaults" />
          <KVTable
            rows={[
              ["Log Driver", spec.TaskDefaults.LogDriver.Name],
              ...(spec.TaskDefaults.LogDriver.Options
                ? Object.entries(spec.TaskDefaults.LogDriver.Options).map(
                    ([k, v]): [string, string] => [`Log Driver: ${k}`, v],
                  )
                : []),
            ]}
          />
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/SwarmPage.tsx
git commit -m "feat: create SwarmPage component"
```

### Task 6: Add Route and Nav Link

**Files:**
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Add import**

Add after the other page imports:
```typescript
import SwarmPage from "./pages/SwarmPage";
```

- [ ] **Step 2: Add "Swarm" to NavLinks**

Change the `links` array in `NavLinks()` to add Swarm as the first entry:
```typescript
    const links = [
        {to: "/swarm", label: "Swarm"},
        {to: "/nodes", label: "Nodes"},
        {to: "/stacks", label: "Stacks"},
        ...
    ];
```

- [ ] **Step 3: Add route**

Add after the `<Route path="/" .../>` line:
```tsx
<Route path="/swarm" element={<SwarmPage />} />
```

- [ ] **Step 4: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 5: Run frontend lint**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "feat: add /swarm route and nav link"
```

### Task 7: Fix Any Existing Test Breakage

**Files:**
- Any test files that call `NewHandlers` (if they exist)

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... && cd frontend && npx vitest run`
Expected: all pass. If any test calls `NewHandlers` with the old signature, update it to pass `nil` for the `SwarmInspector` parameter.

- [ ] **Step 2: Run full lint**

Run: `cd /Users/moritz/GolandProjects/cetacean && make lint`
Expected: no errors

- [ ] **Step 3: Commit (if any fixes needed)**

```bash
git add -A
git commit -m "fix: update test NewHandlers calls for SwarmInspector parameter"
```
