# MCP prompts

**Issue:** [#169](https://github.com/Radiergummi/cetacean/issues/169)
**Related:** [#170](https://github.com/Radiergummi/cetacean/issues/170) (completions — consumes this design's arguments)
**Status:** designed

Offer prompts so an agent knows which sequence of calls answers a question an
operator actually asks.

## What exists today

`internal/mcp/server.go` wires `WithResourceCapabilities` and
`WithToolCapabilities`. `WithPromptCapabilities` is never called and `AddPrompt`
appears nowhere in `internal/mcp/`, so `prompts/list` and `prompts/get` both
return `METHOD_NOT_FOUND`.

The server exposes 27 tools and 12 resources. An agent can call any of them and
nothing tells it which order answers a question. "Why is this service
unhealthy" is five calls in a particular sequence; today every client
rediscovers that order, or the operator drives it turn by turn.

Cetacean's stated goal is to replace the Docker CLI for *understanding* a Swarm
cluster, and the dashboard encodes investigation paths in its navigation —
service detail links to its tasks, tasks to their node, logs beside metrics. The
MCP surface encodes none of that. It is a flat list of capabilities. Prompts are
where that knowledge lives for agent clients.

## Library findings

Three things the issue assumed wrongly or did not know, all confirmed against
`mark3labs/mcp-go` v1.0.0.

**`WithPromptFilter` exists**, with the same signature shape as
`WithToolFilter` (`server/server.go:447`). The issue's concern — "mcp-go would
need an equivalent hook" — does not apply.

**The filter is enforced at both list and get.** `handleGetPrompt`
(`server/server.go:1849`) calls `passesPromptFilters` for the single requested
prompt and, on failure, returns the *same* `prompt not found` error as an
unknown name. So a filtered prompt is indistinguishable from a nonexistent one
and the filter cannot be bypassed by guessing a name. This is the disclosure
posture we want, inherited rather than built.

**`AddPrompt` does not enable the capability.** `prompts/list` and
`prompts/get` are gated on `capabilities.prompts != nil`
(`server/request_handler.go:428,454`), which only `WithPromptCapabilities` sets.
This is precisely the advertise-versus-wire split that produced [E-1] and [E-3]
during the 2026-07-28 upgrade, where `server/discover` promised Tasks and UI
that no capability backed. The mitigation there was behavioural
(`TestTasksExtensionMatchesWiring`), and the same applies here.

[E-1]: 2026-08-30-mcp-2026-07-28-upgrade-plan.md#issues-encountered-running-log
[E-3]: 2026-08-30-mcp-2026-07-28-upgrade-plan.md#issues-encountered-running-log

## Decisions

### Prompts may remediate, not only diagnose

The alternative — diagnostic prompts only — is tempting because it dissolves
operations-tier gating entirely: a tier-0 deployment could offer the whole set.
Rejected because the questions an operator actually asks include "roll this
back" and "drain that node for maintenance", and those are exactly the sequences
where getting the order wrong is expensive. A rollback applied before confirming
the service is actually degraded is an outage the agent caused; a drain applied
before checking that the work can land elsewhere leaves tasks pending.

The cost is accepted deliberately: each remediating prompt must be tier-gated,
and Cetacean ships an opinion about how the remediation should proceed.

### A prompt declares the tools it walks; tier and ACL derive from that

```go
type promptDef struct {
    prompt  mcplib.Prompt
    drives  []string  // tool names this prompt walks, in order
    handler func(context.Context, mcplib.GetPromptRequest) (*mcplib.GetPromptResult, error)
}
```

A prompt's tier is the maximum tier of its driven tools, read from
`toolCatalog()`. Its visibility is "every driven tool is visible to this
caller".

The alternative was a per-prompt `tier` field plus a per-prompt ACL
requirement, mirroring `toolDef` and `toolACLSpecs` directly. Rejected because
it states each prompt's requirements twice — once as prose in the prompt text
naming the tools, once as metadata — and the two drift. Deriving means adding a
tool to a prompt's sequence automatically re-tiers it, and a prompt can never
advertise itself below the tier of a mutation it instructs.

It also yields a free consistency check: a `drives` entry naming a tool absent
from `toolCatalog()` is a typo or a rename, catchable by a table test.

### Visibility is all-or-nothing

A prompt is a sequence. If the caller cannot perform step 4, the runbook
dead-ends after the model has already worked through steps 1 to 3 — and, for a
remediation prompt, possibly after it has already written. Offering a runbook
that cannot be completed is worse than offering none, so a single unavailable
tool hides the prompt.

### Tool-derived visibility is coarse for the diagnostic prompts, and that is correct

`toolACLSpecs` deliberately omits `search`, `list_resources`, `get_topology`,
`get_metrics` and `get_recommendations`: each returns hits across many types,
individually ACL-filtered at call time, so they stay visible to a caller with
grants on only a subset. Only `get_logs` among the reads is gated
(`service:read`).

So deriving visibility from driven tools gives:

| Prompt | Hidden when |
|---|---|
| `diagnose_service` | no `service:read` (via `get_logs`) |
| `explain_unschedulable` | only by the zero-grant floor below |
| `review_capacity` | only by the zero-grant floor below |
| `roll_back_service` | no `service:write` |
| `right_size_service` | no `service:write` |
| `drain_node` | no `node:write` |

Two diagnostic prompts are therefore offered to any caller holding at least one
grant, whatever that grant is. This is consistent rather than broken: the tools
they drive are offered on exactly the same reasoning, and hiding a prompt while
showing every tool it needs would be incoherent. A caller with narrow grants
runs the sequence and gets ACL-filtered results — the same outcome as calling
the tools by hand.

One degenerate case does need handling. `filterToolsForIdentity` has a branch
for an identity with *zero* grants, which drops everything gated and keeps the
ungated reads. Such a caller can read nothing, so seeding an investigation is
pointless: **zero grants means zero prompts.** This reuses the existing branch
rather than introducing a second requirements table, and it is the one place
where "the tools are visible" genuinely fails to imply "the prompt is useful".

Rejected: adding an explicit `needs []aclNeed` to cover the coarse cases.
It reintroduces the second source of truth this design exists to avoid, to fix
an inconsistency that only appears alongside the identical inconsistency in
`tools/list`. If the tool table is later tightened, prompts tighten with it for
free.

### Prompt handlers read no cluster data

A handler interpolates its argument into static text. No cache read, no Docker
call, no resource lookup. Three things follow:

- There is no redaction concern. `cluster.RedactSecret` cannot be bypassed by a
  path that never reads a secret.
- There is no per-resource ACL check at `prompts/get`. Visibility is the entire
  ACL story, which is why the filter has to be right.
- A named resource is **not** validated for existence. The text instructs the
  model to resolve the name with `search` first, which is what `mcpInstructions`
  already tells every client to do. Failing `prompts/get` on a typo would be
  worse: the client has no way to offer a correction, whereas a seeded
  conversation recovers in one call.

### Prompt text is reviewed, not buried

The text is the closest thing Cetacean has to telling a model how to reason
about a Swarm cluster. It lives in its own file as constants — reviewable in one
place beside `mcpInstructions` — under five rules:

1. **Name the tools explicitly, in order.** The sequence is the content; a
   prompt that gestures at "investigate the service" adds nothing to the tool
   list.
2. **State the stopping condition** — what to report instead of continuing.
3. **A remediation prompt must not presume its own diagnosis.** Each opens by
   confirming the problem is real and says to stop and report if it is not.
4. **No claims about *this* cluster.** The text is static and cannot know the
   cluster's shape. It may describe method; it may not assert that the cluster
   runs three managers or uses overlay networks.
5. **One `user` message per prompt.** No fabricated assistant turn.

Rule 4 is the one most easily broken by a well-meaning edit, and the one whose
breakage is hardest to notice: a prompt asserting something false about the
cluster teaches the model a wrong premise it will act on.

## Design

### Components

- **`promptCatalog()`** (`internal/mcp/prompts.go`) — the static `[]promptDef`,
  mirroring `toolCatalog()`.
- **`promptText`** (`internal/mcp/prompt_text.go`) — the message constants, one
  per prompt.
- **`registerPrompts()`** (`internal/mcp/prompts.go`) — derives each prompt's
  tier, skips those above the effective operations level, records the rest on
  `s.registeredPrompts`, and calls `AddPrompt`.
- **`filterPromptsForIdentity()`** (`internal/mcp/server.go`) — the
  `WithPromptFilter`, beside `filterToolsForIdentity`. Returns every prompt when
  the ACL is absent or the policy is nil; returns **none** when the identity
  holds zero grants; otherwise keeps a prompt iff every name in its `drives`
  passes `toolVisibilityFor`.
- **`toolVisibilityFor(ctx) func(name string) bool`** — extracted from
  `filterToolsForIdentity`, returning nil for allow-all. Shared by both
  filters.

### The one refactor

`filterToolsForIdentity` currently builds `allowedByType` inline and passes a
closure to `filterToolsByACLSpec`. Extracting the closure as
`toolVisibilityFor` lets the prompt filter ask "would this tool be visible?"
without duplicating the grant-flattening logic, including the `write` implies
`read` rule and the `*` wildcard handling.

This removes duplication rather than adding a seam. The per-prompt-metadata
alternative would have needed a second copy of the same reasoning.

### Registration and capability

`WithPromptCapabilities(false)` — no `listChanged`, since the catalog is static
and cannot change while the process runs.

Being straight about one thing: the three diagnostic prompts are tier 0, there
is no operations level below 0, and no prompt is gated on anything else, so the
registered set is never empty. A conditional `if len(registered) > 0` would be
unreachable, and this codebase does not add error handling for impossible
states. The property is asserted behaviourally instead — a test that
`prompts/list` is non-empty and `prompts/get` succeeds — so if a prompt later
becomes conditional (say `review_capacity` gated on Prometheus being
configured), that test is where the mismatch surfaces.

### The prompts

| Name | Tier | Drives | Argument |
|---|---|---|---|
| `diagnose_service` | 0 | `search`, `list_resources`, `get_logs`, `get_metrics`, `get_recommendations` | `service` |
| `explain_unschedulable` | 0 | `search`, `list_resources` | `service` |
| `review_capacity` | 0 | `list_resources`, `get_metrics`, `get_recommendations` | — |
| `roll_back_service` | 1 | `search`, `list_resources`, `rollback_service` | `service` |
| `right_size_service` | 2 | `search`, `get_metrics`, `get_recommendations`, `update_service_resources` | `service` |
| `drain_node` | 3 | `search`, `list_resources`, `update_node_availability` | `node` |

One remediation prompt per tier above 0, which is a property worth keeping: a
tier-0 deployment offers exactly the three diagnostic prompts and each higher
tier adds one, so the gating is observable rather than theoretical.

**Rejected candidates.** *Triage recommendations by severity* — one
`get_recommendations` call with a severity filter, and the recommendations
widget already groups by severity; a prompt adds nothing but indirection.
*Summarise recent changes* — one `cetacean://history` read, too thin to stand
alone, so it becomes step 5 of `diagnose_service`.

### Prompt text

Verbatim, for review. `{service}` and `{node}` are the interpolated arguments.

#### `diagnose_service`

> A service in this Swarm cluster is reported unhealthy: `{service}`.
>
> Work through this order and stop as soon as you can name the cause.
>
> 1. Resolve the name with `search` to get the service ID, then read
>    `cetacean://services/<id>` for its mode, desired replicas and update status.
> 2. Call `list_resources` for tasks and find this service's tasks. Compare the
>    running count against desired. Note any task in `failed`, `rejected` or
>    `orphaned`, and its `Status.Err`.
> 3. For the most recent failing task, call `get_logs` on the service. A crash
>    loop usually names itself in the last lines before exit.
> 4. If the tasks are running but the service still misbehaves, call
>    `get_metrics` for cpu and memory over the last hour. A container at its
>    memory limit is killed and restarted without ever logging why.
> 5. Read `cetacean://history` for changes to this service. A fault that started
>    minutes after an image update is usually the update.
> 6. Call `get_recommendations` and report any finding naming this service.
>
> Report the cause, the evidence you used, and what you would change. Do not
> apply a change as part of this investigation.

#### `explain_unschedulable`

> Service `{service}` has tasks Swarm has not placed. Four independent causes
> produce this one symptom; check them in this order, because the cheap checks
> rule out the expensive ones.
>
> 1. Resolve the name with `search`, then read `cetacean://services/<id>` and
>    note `Spec.TaskTemplate.Placement` — constraints, preferences and
>    `MaxReplicas`.
> 2. Call `list_resources` for tasks, filter to this service, and read the
>    pending task's `Status.Err` and `Status.Message`. Swarm often states the
>    reason outright ("no suitable node"); if it does, stop there.
> 3. Call `list_resources` for nodes. For each placement constraint, check which
>    nodes satisfy it — `node.labels.*` come from a node's `Spec.Labels`,
>    `node.role` and `node.hostname` from its description. A constraint naming a
>    label no node carries can never be satisfied.
> 4. Check each node's `Availability`. A `drain` node accepts nothing and a
>    `pause` node accepts no new tasks, so a cluster can look healthy and still
>    have nowhere to put this service.
> 5. Compare the task template's resource reservations against what nodes have
>    left. Reservations, not limits, decide placement.
>
> Report which of the four it is, and the specific constraint, label or
> reservation responsible. If several apply, say so — fixing one leaves the task
> pending.

#### `review_capacity`

> Review whether this Swarm cluster has capacity headroom.
>
> 1. Read `cetacean://cluster` for the node count and aggregate capacity.
> 2. Call `list_resources` for nodes and note each node's role, availability and
>    resources. Manager nodes running workloads are a resilience risk even when
>    they have room.
> 3. Call `get_metrics` for each node's cpu and memory over the last day, or the
>    last week if load is weekly. Compare actual use against reservations rather
>    than limits: Swarm schedules on reservations, so a cluster can refuse to
>    place a task while sitting mostly idle.
> 4. Call `get_recommendations` and report every sizing and cluster finding.
> 5. State the headroom as "the largest service that could still be placed",
>    not as a percentage — a percentage hides fragmentation across nodes.
>
> Report where the cluster is constrained, which nodes are the constraint, and
> whether the limit is reservations or real use. Do not change any reservation
> as part of this review.

#### `roll_back_service`

> You have been asked to roll `{service}` back to its previous version. Confirm
> the rollback is warranted before performing it.
>
> 1. Resolve the name with `search`, then read `cetacean://services/<id>`. Check
>    `UpdateStatus`: a service already `rolling_back` needs no second rollback,
>    and one still `updating` may converge on its own.
> 2. Call `list_resources` for tasks and confirm this service actually has
>    failing or unplaced tasks. If every task is running and healthy, stop and
>    report that — there is nothing to roll back, and rolling back a healthy
>    service is an outage you caused.
> 3. Read `cetacean://history` to confirm what changed and when. A rollback
>    undoes the last spec update, which may not be the change that broke it.
> 4. Call `rollback_service` with `task.ttl` set, so the call reports when the
>    previous version's replicas are actually running rather than when Docker
>    accepted the request.
> 5. Poll `tasks/get` until the task settles. On failure read `statusMessage`: a
>    rollback can fail for the same reason the update did.
>
> Report the version rolled back to and the running replica count afterwards. If
> step 2 showed a healthy service, report that and make no change.

#### `right_size_service`

> Right-size the resource reservations for `{service}`, using measured use
> rather than a guess.
>
> 1. Call `get_recommendations` and find the sizing finding for this service.
>    Cetacean computes it from observed use over the configured lookback window.
>    If there is no finding, the service is already within thresholds — say so
>    and stop.
> 2. Resolve the name with `search`, then read `cetacean://services/<id>` for
>    its current `Resources.Reservations` and `Resources.Limits`.
> 3. Call `get_metrics` for cpu and memory over the last week and check the
>    finding against the series yourself. A service whose load is weekly or
>    bursty can look over-provisioned over a day and be correctly sized over a
>    week.
> 4. Apply the change with `update_service_resources`. Raise reservations before
>    lowering limits, and never set a reservation above a limit.
> 5. Confirm the tasks rescheduled and are running. Changing reservations
>    replaces every task, so this is a rolling restart, not a metadata edit.
>
> Report the old and new values and the evidence for them. If the metric window
> disagrees with the recommendation, report the disagreement and change nothing.

#### `drain_node`

> Drain node `{node}` for maintenance, without moving work somewhere it cannot
> run.
>
> 1. Resolve the name with `search`, then read `cetacean://nodes/<id>` for its
>    role, current availability and resources.
> 2. If this node is a manager, read `cetacean://cluster` for the manager count
>    first. Draining a manager does not remove it from the raft quorum, but
>    losing it while drained does — and a cluster with two managers has no
>    quorum tolerance at all. Report and stop if draining would leave fewer than
>    three managers, unless you have been told otherwise.
> 3. Call `list_resources` for tasks and list every task currently on this node,
>    with its service. This is the work that has to land elsewhere.
> 4. Call `list_resources` for nodes and check that the remaining `active` nodes
>    satisfy each of those services' placement constraints and have the
>    reservations spare. A service constrained to this node alone has nowhere to
>    go and will sit pending.
> 5. Only then call `update_node_availability` with `drain`.
> 6. Poll the affected services' tasks until they are running elsewhere. Report
>    any that stayed pending, with the constraint that blocked them.
>
> Report what moved, where it moved to, and anything that could not move. If
> step 2 or 4 found a blocker, report it and leave the node `active`.

### Completions seam (#170)

Every prompt but `review_capacity` takes one resource name, which is where
completion is most visible. This design registers no completion provider —
that is #170's scope. It only ensures the argument names are the obvious ones
(`service`, `node`) so a provider can key off them without a mapping table.

### Suggested implementation split

Two batches, reviewable independently, in this order:

- **Batch A** — the mechanism plus the three tier-0 diagnostic prompts:
  `toolVisibilityFor` extraction, `promptCatalog`, `registerPrompts`,
  `filterPromptsForIdentity`, the capability, and the full test set. Landing
  this alone leaves a coherent, shippable feature.
- **Batch B** — the three remediation prompts. Pure catalog and text additions
  against a mechanism already proven, and the batch where the tier-gating tests
  earn their keep.

## Testing

- **Expanded messages, per prompt.** `prompts/get` with a known argument
  returns messages matching the constant with the argument interpolated. This is
  the issue's criterion — wording is reviewable and cannot drift silently.
- **Tier gating.** At operations level 0, `prompts/list` returns exactly the
  three diagnostic prompts; at 3, all six. Asserted by name, not by count, so a
  prompt moving tier fails visibly.
- **ACL, both directions.** An identity with `service:read` only sees the
  diagnostic prompts and not `roll_back_service`; an identity with zero grants
  sees none. `prompts/get` on a prompt the caller cannot see returns *not
  found*, not *forbidden* — inherited from mcp-go, pinned because it is a
  disclosure property.
- **Drift guard.** A table test asserting every `drives` entry names a tool in
  `toolCatalog()`. Renaming a tool then fails the suite instead of silently
  mis-tiering a prompt or hiding it forever.
- **Capability wiring.** `prompts/list` is non-empty and `prompts/get`
  succeeds, driven through the real transport. A unit test on `promptCatalog()`
  alone would pass with `WithPromptCapabilities` unwired — the same altitude
  mistake as [E-2], where Phase 2's tests passed against a synthetic session
  while the real transport dropped every subscription.

[E-2]: 2026-08-30-mcp-2026-07-28-upgrade-plan.md#issues-encountered-running-log

## Out of scope

- **Completions** for prompt arguments or templated resource URIs → #170.
- **`listChanged` notifications.** The catalog is static.
- **Prompts that read cluster state** to pre-fill their text. It would make
  every prompt an ACL surface and a redaction surface, for the benefit of
  saving the model one `search` call.
- **Operator-authored prompts** from config. No demand established, and it
  would make prompt text an injection surface aimed at the model.

## Documentation

`docs/mcp.md` gains a Prompts section listing each prompt, its tier and its
argument. `CLAUDE.md`'s `mcp/` architecture note gains the derivation rule.
CHANGELOG entry under Added, written from the operator's view: their agent
client offers named investigations rather than a flat tool list.
