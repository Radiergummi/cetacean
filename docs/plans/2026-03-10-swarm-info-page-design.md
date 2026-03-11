# Swarm Info Page — Design Spec

## Goal

Add a `/swarm` page that displays Docker Swarm cluster metadata from `SwarmInspect()`: cluster ID, join tokens (with reveal toggle), orchestration settings, raft config, CA config, dispatcher, encryption, TLS info, and network defaults.

## Architecture

New `GET /api/swarm` endpoint calls `docker.SwarmInspect()` directly (no caching — this is slow-changing config). The handler needs a new `SwarmInspector` interface on `Handlers` (similar to `DockerLogStreamer`) to keep testability. Frontend gets a new `SwarmPage` component following the same pattern as `ConfigDetail` (fetch on mount, InfoCard grid + sections). No SSE subscription needed — Docker doesn't emit events for swarm spec changes.

## Data Shape

From `swarm.Swarm` (Docker SDK v28.5.2):

```
Swarm
├── ClusterInfo
│   ├── ID                        string
│   ├── Meta.CreatedAt            time.Time
│   ├── Meta.UpdatedAt            time.Time
│   ├── Spec
│   │   ├── Annotations (Name, Labels)
│   │   ├── Orchestration.TaskHistoryRetentionLimit  *int64
│   │   ├── Raft (SnapshotInterval, KeepOldSnapshots, LogEntriesForSlowFollowers, ElectionTick, HeartbeatTick)
│   │   ├── Dispatcher.HeartbeatPeriod               time.Duration
│   │   ├── CAConfig (NodeCertExpiry, ExternalCAs, ForceRotate)
│   │   ├── TaskDefaults.LogDriver                   *Driver (Name, Options)
│   │   └── EncryptionConfig.AutoLockManagers        bool
│   ├── TLSInfo (TrustRoot, CertIssuerSubject, CertIssuerPublicKey)
│   ├── RootRotationInProgress    bool
│   ├── DefaultAddrPool           []string
│   ├── SubnetSize                uint32
│   └── DataPathPort              uint32
└── JoinTokens
    ├── Worker   string
    └── Manager  string
```

## Frontend Layout

**Route:** `/swarm`
**Nav:** "Swarm" link inserted before "Nodes" in NavLinks

### Sections (top to bottom)

1. **PageHeader** — title "Swarm", no breadcrumbs (top-level page)

2. **Overview** (InfoCard grid)
   - Cluster ID (truncated via `ResourceId` component)
   - Created (via `Timestamp`)
   - Updated (via `Timestamp`)
   - Default Address Pool (comma-joined)
   - Subnet Size (`/<size>`)
   - Data Path Port

3. **Join Tokens** — two rows, each with a masked token and reveal/copy button. On reveal, show full `docker swarm join --token <TOKEN> <manager-addr>` command. Manager address comes from the node cache (find the leader node's ManagerStatus.Addr). If not available, just show the raw token.

4. **Orchestration** — KVTable
   - Task History Retention Limit

5. **Raft** — KVTable
   - Snapshot Interval, Keep Old Snapshots, Log Entries for Slow Followers, Election Tick, Heartbeat Tick

6. **CA Configuration** — KVTable
   - Node Certificate Expiry (formatted duration)
   - Force Rotate count
   - Root Rotation In Progress (yes/no)
   - External CAs (if any — URL + protocol)

7. **Dispatcher** — KVTable
   - Heartbeat Period (formatted duration)

8. **Encryption** — KVTable
   - Auto-Lock Managers (yes/no)

9. **Task Defaults** — KVTable (only if LogDriver is set)
   - Log Driver name + options

## Backend API

**`GET /api/swarm`** returns `swarm.Swarm` as JSON directly (the Docker SDK type serializes cleanly). Also includes `managerAddr` from cache for join command display.

Response shape:
```json
{
  "swarm": { ... },         // swarm.Swarm from Docker SDK
  "managerAddr": "10.0.0.1:2377"  // leader's ManagerStatus.Addr from cache, or ""
}
```

## Interface Changes

The `Handlers` struct currently uses `DockerLogStreamer` (a narrow interface). We add a second interface:

```go
type SwarmInspector interface {
    SwarmInspect(ctx context.Context) (swarm.Swarm, error)
}
```

`docker.Client` already has the underlying `SwarmInspect` method via the Docker SDK — we just expose it.
