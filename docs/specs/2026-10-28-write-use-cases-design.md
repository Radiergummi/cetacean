# Write use cases in one place — design

Closes the 2026-10-27 architecture review's A001 stage 3, A045 and A046.

## Problem

Every write the dashboard and an agent can both make is implemented twice: once as a REST
handler behind a middleware chain, once as an MCP tool. Stages 1 and 2 of A001 moved the
pieces that were easiest to share into `internal/cluster` — resolving a key, the ACL resource
it is checked as, the tier of each operation — and the host-access rules were already there.
What is still written twice is everything around them:

- decoding aside, both transports re-implement the ACL check, the tier check, the version
  precondition, the host-access guard, the Docker call, and the classification of the
  engine's error;
- each keeps its own set of Docker-writer interfaces under the same names
  (`ServiceLifecycleWriter`, `ServiceSpecWriter`, `NodeWriter`, `ResourceRemover`,
  `DockerWriteClient`) with different method sets, and `docker.Client` carries five
  one-line section adapters only MCP calls;
- `internal/cluster`, the package that is supposed to mean "both transports apply this",
  also holds about thirty projections only MCP renders (digests, graphs, impacts, the
  cluster status), so its boundary no longer says anything.

Writing a rule twice has produced divergence each time it was checked. On `main` today:

| | REST | MCP |
|---|---|---|
| A write to a resource the caller cannot read | `403 ACL002` | "not found" |
| An unclassified engine error | withheld as `ENG004`; text shown only for invalid-argument (`ENG003`) and unavailable (`ENG001`) | text passed through for not-found, invalid-argument, unavailable, conflict, failed-precondition, out-of-sequence |
| Scaling a global service | `SVC005` from a cache pre-check | the engine's invalid-parameter text |
| Env patch on a service holding host access | re-checked inside the writer against Docker's live env | checked against the cached spec; relies on the version pin |

## Proposal

Three moves, in this order.

### 1. Projections leave `cluster` (A046)

A projection one transport renders lives in that transport. The ~30 MCP-only symbols — the
digests (`digests.go`, `view.go`), the graphs (`topology.go` beyond `DrainImpactGraph`), the
impacts (`impact.go`), the cluster status (`status.go`), the timeline, `ServiceConverged`,
`DeriveServiceState` and the MCP-only resolution helpers — move into `internal/mcp`. The
API-only ones (`ServiceLinks`, `StackLinks`, `VersionDetail`, `CheckPluginSettings`, …) move
into `internal/api`. Anything both read stays. `.claude/ARCHITECTURE.md` already states the rule;
after this the package obeys it, and what is left is the shared core the next two moves
build on.

Mechanical: no behaviour changes, one PR.

### 2. One use case per operation, in `cluster`

`cluster.Writes` performs each of the 18 operations both transports offer — the ones in the
`Op*` table — end to end:

```go
// Writes performs the cluster's writes for either transport.
type Writes struct {
	cache  *cache.Cache
	docker Writer
}

// Caller is who asks, as a transport established it.
type Caller struct {
	CanRead, CanWrite func(resource string) bool
	Level             config.OperationsLevel
}

func (w *Writes) ScaleService(
	ctx context.Context, c Caller, key string, replicas uint64, version *swarm.Version,
) (swarm.Service, error)
```

A use case resolves the key (`Lookup`), refuses an unreadable target as not found and an
unwritable one as denied, checks the tier, applies the domain rules (global services do not
scale, the image is trimmed, host access held needs level 3), pins the version when one is
given, calls Docker, and classifies what comes back. It returns the resulting resource or a
`*cluster.WriteError`.

The transports keep what is genuinely theirs:

- **REST**: decoding (JSON Patch and merge patch both become a `func(current) (next, error)`
  mutator), turning `If-Match` into a version, `Prefer: wait`, rendering, and mapping a
  `WriteError` to a problem code. The router chains stay: they are what the `Allow` header and
  `OPTIONS` read, and they refuse early. The use case re-checks; it is the authority.
- **MCP**: argument decoding, elicitation and the impact text it asks with, tasks, and mapping
  a `WriteError` to tool error text. The tier a tool is registered at stays fixed at
  registration (`mcp.operations_level`), as documented; MCP passes that level as the caller's.

Writes only one transport offers — swarm settings, plugins, stacks, endpoint mode, networks
on a service — stay in `internal/api`: there is nothing to keep in step.

### 3. One writer interface, owned by `cluster` (A045)

`cluster.Writer` is the one interface the use cases call, declared where it is consumed. Its
surface is the service lifecycle, `UpdateServiceSpec(ctx, id, mutate)`, the node writes,
creates, removals and label updates. The per-section adapters on `docker.Client`
(`UpdateServiceEnv`, `UpdateServiceResources`, `UpdateServiceUpdatePolicy`, …, five of which
only MCP calls) become spec mutators inside `cluster`, next to the `*Change` functions that already describe each
section. `internal/api` and `internal/mcp` stop holding a Docker writer at all for these
operations, so both interface sets go.

## Errors

```go
// WriteError is why a write did not happen, in terms both transports can map.
type WriteError struct {
	Kind     WriteErrorKind
	Resource string // "service", "config", …
	Detail   string // safe to show the caller
	Err      error  // the cause, for the log
}
```

| Kind | REST | MCP |
|---|---|---|
| `NotFound` (absent, or unreadable) | the resource's not-found code (`SVC003`, `CFG002`, …) | "not found" |
| `Denied` | `ACL002` | "write access denied" |
| `BelowTier`, `HostAccess` | `OPS001` | the detail |
| `Invalid` | the resource's code (`SVC004`, `SVC005`, …), else `ENG003` | the detail |
| `Conflict` | the resource's conflict code (`SVC001`, `CFG001`, …) with the detail | the detail |
| `Stale` | `API013` when the version came from `If-Match`, else the conflict code | "changed since you read it" |
| `Unavailable` | `ENG001` | the detail |
| `Engine` | `ENG004`, detail withheld and logged | a generic refusal, detail logged |

The engine classifier (`docker` error classes, out-of-sequence, removal conflicts) is
written once, in `cluster`, and decides what `Detail` may say. That is where the disclosure
rule in the table above stops being two rules.

## Decisions this settles

All five accepted by the maintainer on 2026-10-28.

1. **An unreadable target answers as not found in both transports.** REST changes from
   `403 ACL002` to the resource's 404 for a write the caller cannot even read, so a write no
   longer confirms that a hidden resource exists. A readable but unwritable target stays
   `403`. Breaking for a client that keys on the 403; CHANGELOG entry.
2. **One disclosure rule for engine errors.** The classes REST shows today (invalid argument,
   unavailable) plus conflict text in the conflict code's detail; everything else withheld.
   MCP stops passing raw not-found, failed-precondition and out-of-sequence text.
3. **Scaling a global service is `Invalid` from the cache in both**, before Docker is called.
4. **A host-access-held change is always re-checked inside the writer** against Docker's
   live spec, in both transports. The version pin stays for the precondition it expresses.
5. **The tier stays per transport as documented**: REST per request with the token ceiling,
   MCP at registration. The use case takes the level it is handed and does not reconcile them.

## Staging

1. **A046** — move the projections out of `cluster`. Mechanical.
2. **Errors** — `WriteError`, the kinds, and the single engine classifier; both transports map
   their existing write errors through it. Settles decision 2.
3. **Service lifecycle** — scale, image, rollback, restart as use cases, `cluster.Writer`
   introduced for them. Settles decisions 1 and 3.
4. **Service sections** — `update_service` and the REST section routes onto mutators; the
   docker section adapters go. Settles decision 4.
5. **Nodes, removals, creates, labels** — the rest of the 18; the `api` and `mcp` writer
   interface sets are deleted (A045).

Each step keeps `make check` green and the contract tests' cross-transport invariants passing;
step 3 onward adds a contract case per settled divergence, asserting both transports answer
it alike.

## Not doing

- **Moving REST-only writes into `cluster`.** A rule with one consumer is not shared.
- **A command bus or generic `Execute(op, args)`.** Eighteen typed methods are easier to read
  and to grep than a dispatcher, and the transports already know which one they call.
- **Changing what MCP's tool list shows by caller.** Registration-time tiers are documented
  behaviour; a per-call tier for MCP is a separate decision.
