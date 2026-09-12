# MCP Agent Surface Design

**Date:** 2026-09-04

## Summary

Reshape Cetacean's MCP surface around the questions operators actually ask, rather than around Docker Engine endpoints.

Three things change. Reads stop returning raw `swarm.Service` objects and return a compact, self-describing representation instead. Four read verbs are added — cluster status, a universal digest, a change timeline, and a server-side wait — so that questions currently needing several calls and client-side joining need one. The eight tier-2 service spec editors merge into a single `update_service_config`, which also picks up healthcheck and command; secret references and mounts arrive as separate tier-3 tools, and secrets and configs become creatable.

This is derived from a corpus of 67 user intents (below). 24 of them cannot be answered by the current surface at all, and a further 18 are answerable only obliquely and expensively.

## Motivation

Measured against a live cluster of 8 services on 2026-09-04:

- `list_resources(services)` returned **~14k tokens** for 8 services. The payload is raw `swarm.Service`: sixteen `Platforms` entries per nginx service, a full `PreviousSpec` clone for anything recently updated, `VirtualIPs`, `CredentialSpec: null`. Extrapolated to the tool's own 200-record cap, one call is ~350k tokens.
- The answer is not in that payload. `state` is derived from tasks, so the most expensive read cannot answer the most common question.
- "Are any services broken?" has no tool. It was answerable only via `get_topology(placement)`, a graph tool that happens to carry derived state.
- "Anything in the logs in the last five minutes?" across the cluster requires enumerating services and then one call each, client-driven, unbounded.
- "Update the secret of foo" is impossible: `remove_secret` exists, `create_secret` does not.

The current surface has 27 tools: 6 reads and 21 writes (5 tier 1, 9 tier 2, 7 tier 3), 11 of the writes being one-knob editors. The ratio is inverted relative to where the leverage is.

## The Corpus

Grouped by the moment the user is in, not by resource type. **✗** marks what the surface cannot answer today; **~** marks oblique or expensive.

**Landing** — 1. Is the cluster healthy? ~ · 2. Any services with problems? ~ · 3. What broke overnight? ~ · 4. Anything mid-rollout? ~ · 5. Nodes down or draining? ~ · 6. Near capacity? ✗ · 7. Morning summary ~

**Triage** — 8. Why is X failing? · 9. Why won't X schedule? ~ · 10. Why is X restart-looping? · 11. Which replica is broken? · 12. App or infrastructure? · 13. The reason, not the state ~ · 14. New, or always like this? ~

**Deploy & verify** — 15. Has X deployed yet? ~ · 16. Rollout converged? ~ · 17. Deploy v2 and tell me when live · 18. Watch X until deployed or failed ✗ · 19. What image is X running? · 20. Roll back; did it work? · 21. Diff current vs previous spec ✗ · 22. Deploy a stack ✗

**Logs** — 23. What errors is X showing? · 24. Anything cluster-wide in five minutes? ✗ · 25. Every error in the last hour ✗ · 26. Grep the cluster ✗ · 27. Tail X while I reproduce ✗ · 28. Logs of the container that just died ✗ · 29. What else happened at 14:02? ✗

**Capacity** — 30. What's using the most CPU/memory? ✗ · 31. Is X mis-provisioned? · 32. Can I fit five more replicas? ✗ · 33. Why is node N hot? · 34. Volumes filling up? ✗ · 35. Right-size X ~

**Relationships** — 36. What can reach what? · 37. What runs where? · 38. Who uses secret Y? ✗ · 39. If I drain N, what moves? ✗ · 40. What's in stack S? ~ · 41. What's exposed publicly? ✗

**Inventory** — 42. Env vars of X? · 43. Limits, constraints, ports, mounts? · 44. Orphaned secrets and configs? ✗ · 45. Which images? Anything on `:latest`? ✗ · 46. Which services lack healthchecks? ~ · 47. Labels on node N?

**Routine mutations** — 48. Scale/restart/update/roll back ✓ · 49. Remove failed tasks ✓ · 50. Drain and restore a node ✓ · 51. Promote/demote ✓ · 52. Label a node ✓

**Configuration** — 53. Update the secret of foo ✗ · 54. Add an env var ✓ · 55. Raise memory limit ✓ · 56. Add a healthcheck ✗ · 57. Change command/args ✗ · 58. Mount a volume ✗ · 59. Publish a port ✓

**Compound** — 60. Rotate a password everywhere ✗ · 61. Restart a whole stack ~ · 62. Scale the dev stack to zero ~ · 63. Fix all missing healthchecks ✗ · 64. Apply a recommendation ~

**Meta** — 65. What can I see and change? · 66. Why was that refused? ~ · 67. Explain this recommendation

## Design Principles

Four patterns fall out of the corpus, each indicting a different property of the current surface.

**A read must carry its reason, not only its state.** `demo_stuck` reporting `"state": "failed"` is worthless; *"no node satisfies `node.labels.gpu == true`"* is the whole answer, and Swarm already holds it in the task's status message. A state without a cause guarantees a follow-up call, and follow-up calls are where the token budget actually goes.

**There are six answer shapes, not eight resource types.** Overview, digest-of-one, list-of-many, timeline, series, graph. One entry point per Docker endpoint is why the most common question has no home.

**Half the intents are cross-resource; none of the tools are.** Intents 24–26, 30, 38, 39, 41, 44, 45, 60 all require enumerate-then-fan-out, driven by the model and paid for in its context. The server can do these joins in-process for nearly nothing.

**Time is a first-class dimension and it is missing.** Intents 3, 14, 21, 24, 25, 29. `cetacean://history` exists but is unfilterable from the tool surface, and nothing correlates a log timestamp with a change event — which is intent 29, the highest-value question in incident response.

**Units are named, never implied.** Every numeric field carries its unit in its name (`memoryLimitBytes`, `cpuLimitCores`) or is a duration string (`"10s"`). This is a direct consequence of the CPU unit defect fixed in `e25089e2`, where `configured: 25` and `suggested: 50000000` were the same quantity in one record.

## Approach

Three options were considered.

**A. Additive** — keep everything, add compact tools alongside. Lowest risk, no breakage, but the surface grows to ~35 tools and the model must choose between `list_resources` and its compact twin on every call. Introspection cost worsens.

**B. Reshape** *(chosen)* — compact by default, raw behind an explicit opt-in; merge the over-decomposed writes; add the missing verbs.

**C. Question-shaped rewrite** — discard resource mirroring entirely.

B is chosen because C is B plus churn: `get_topology`, `get_recommendations` and the four converging mutations are already question-shaped and need no change. B's cost is breaking the widget contract, which is a cost worth paying — the widget is the one consumer controlled on both sides.

## The Compact Representation

Two shapes, used everywhere.

### Row — the unit of a list

Modelled on `cluster.TopologyNode`, which already proved sufficient for diagnosis in practice.

```json
{
  "id": "csv69qvqymkkocjw532tj9tl9",
  "name": "cetacean_cetacean",
  "type": "service",
  "stack": "cetacean",
  "state": "running",
  "detail": "cetacean:latest",
  "desired": 1,
  "running": 1
}
```

`state` is the derived state, not a raw Docker enum. `detail` is the type's most identifying secondary fact (image for a service, role for a node, driver for a network). `desired`/`running` are present only where they mean something. Every row carries both `id` and `name`, following the `targetId`/`targetName` pattern the recommendations already use, so a caller never has to resolve one into the other.

### Digest — the unit of `describe`

A tight envelope plus a type-specific body:

```json
{
  "id": "tjwkds12i7iff9q0kg8o19azm",
  "name": "demo_stuck",
  "type": "service",
  "state": "failed",
  "reason": "no node satisfies constraint node.labels.gpu == true",
  "since": "2026-09-04T16:15:11Z",
  "details": { },
  "related": [
    { "id": "…", "name": "demo_overlay", "type": "network", "relation": "attached-to" }
  ],
  "recentFailures": [
    { "taskId": "…", "at": "2026-09-04T17:44:08Z", "state": "rejected", "message": "no suitable node" }
  ]
}
```

`reason` is the load-bearing field and is populated from the task status message, the unmet constraint, or the update status — whichever explains the current `state`. It is absent when `state` is healthy.

`details` is type-specific and deliberately open in the output schema; the envelope is tight. This is the one place the design accepts a loose schema, and it is a deliberate trade: eight `describe_<type>` tools would tighten the schema at the cost of eight entries in every `tools/list`. For a service, `details` holds mode, replicas, image, ports, networks, mounts, env keys (**names only, never values** — env carries credentials), resources in named units, healthcheck presence and interval, placement constraints, and update/rollback policy.

### `describe` and the resources coexist

`describe` does **not** replace the templated `cetacean://<type>/{id}` resources. Those are subscribable — `NotificationManager` delivers `notifications/resources/updated` per URI — and a tool cannot carry a subscription, so replacing them would silently drop live updates for every widget and client that relies on them.

Instead the resources keep their URIs and their subscriptions, and their *payload* becomes the digest. `describe` is the tool form of the same shape, exactly as `get_recommendations` is the tool form of `cetacean://recommendations`. One builder feeds both, so the two cannot diverge.

### Raw escape hatch

`list` and `describe` accept `raw: true`, returning the unmodified Docker object. It exists so nothing becomes unreachable, and is documented as expensive. The widget does not use it (see Widget Migration).

## Read Tools

| Tool | Replaces / adds | Serves |
|---|---|---|
| `get_cluster_status()` | new | 1–7 |
| `describe(type, id, raw?)` | tool form of the 8 templated `cetacean://` reads | 8–14, 19, 31, 38, 40, 42, 43, 47 |
| `find(type?, query?, state?, stack?, node?, image?, label?, limit?, offset?, raw?)` | merges `search` + `list_resources` | 40, 41, 44, 45, 46 |
| `get_logs(service? \| task? \| stack? \| cluster?, since?, level?, contains?, tail?)` | extends `get_logs` | 23–28 |
| `get_events(since?, until?, types?, resource?)` | `cetacean://history` as a tool | 3, 14, 21, 29 |
| `get_metrics(...)` | + `target: "cluster"`, + `top` | 30, 33 |
| `get_topology(view)` | + `view: "drain-impact"` | 36, 37, 39 |
| `get_recommendations(...)` | unchanged | 31, 35, 64 |

`get_cluster_status` is the landing call and must name the unhealthy things, not count them. Shape: the existing counts, plus `unhealthyServices[]` and `unhealthyNodes[]` as rows carrying `reason`, plus `rollouts[]` in flight, plus `capacity` pressure.

`get_logs` gains `task` in this branch already (`e25089e2`). This spec adds `stack` and `cluster` scope with server-side merge, a per-service cap, `contains` for grep, and a combined cursor. The retention caveat already documented for `task` stays.

`get_events` and `get_logs` return the same timeline shape so intent 29 — "this started at 14:02, what else happened?" — is one interleaved read rather than a correlation the model performs.

## `watch` — the single biggest saving

```
watch(target, until, timeout) -> { outcome, observed, elapsed }
```

Task-augmented, reusing `awaitConvergence` from `internal/mcp/tasks.go`. Conditions: `converged`, `healthy`, `failed`, `log_matches`. Turns intents 18 and 27 from a twenty-poll loop into one call, and exposes `cluster.ServiceConverged` as a read — today the convergence rule is reachable only as a side effect of a mutation, so an agent cannot ask "has it deployed yet" without deploying something.

Same detached-context caveat as the existing converging mutations: `tasks/cancel` cannot interrupt the wait, and `timeout` is the real bound.

## Write Tools

### The tier rule

> **A tool is the unit of visibility.** `tools/list` is filtered by operations tier *and* by `toolVisibilityFor`'s per-type ACL check. If a caller can see a tool, every part of it must be callable. Collapse only within one tier **and** one target resource type. Never compute a tier at call time.

This rule is why the six `remove_*` tools are **not** merged: their tier is uniform (3), but per-type ACL visibility is not — today a caller with no secret grant never sees `remove_secret`, whereas a merged `remove` would be visible to anyone with any removal grant and would reject at call time on secrets. A tier surprise and an ACL surprise are equally bad.

### Changes

| Tier | Tool | Change |
|---|---|---|
| 1 | `scale_service`, `update_service_image`, `rollback_service`, `restart_service`, `remove_task` | unchanged — distinct verbs, distinct arguments |
| 2 | **`update_service_config(id, changes)`** | merges `env`, `labels`, `resources`, `placement`, `ports`, `update_policy`, `rollback_policy`, `log_driver`; adds `healthcheck`, `command` |
| 2 | `update_node_labels` | unchanged |
| 2 | **`create_secret`**, **`create_config`** | new; additive, no privilege change |
| 3 | **`update_service_secrets`**, **`update_service_configs`** | new; changes which credentials a container receives |
| 3 | **`update_service_mounts`** | new; a host bind mount is a privilege escalation path |
| 3 | node availability, node role, 5× `remove_*` | unchanged |

Secret references and mounts sit at tier 3 rather than joining `update_service_config`, because their tier differs and the rule says a differing tier means a different tool. Mounts especially: binding `/var/run/docker.sock` turns a configuration change into a root shell.

`create_secret` plus `update_service_secrets` together close intent 53. Swarm secrets are immutable, so rotation is create-new → repoint service → drop old; Cetacean could previously only do the third step.

## Widget Migration

`frontend/src/widgets/table/columns.ts` maps accessors over raw Docker field names (`Spec.Name`, `Description.Hostname`). Moving `find`/`list` to rows breaks it, and the two change together in one commit.

The row shape is strictly easier for the widget: `columns.ts` loses its per-type accessor table and its name/ID fallback for unknown types, because every row already carries `id`, `name`, `state` and `detail` under those names. The widget does not use `raw: true` — a widget rendering raw Docker objects is what motivated the raw wire format in the first place, and that is the coupling being removed.

## Out of Scope

The compound intents (60–64) are deliberately excluded. "Rotate this password everywhere it is used" is an agent composing primitives, not a tool, and it needs `create_secret` and `update_service_secrets` to exist first. Once they do, it falls out.

These, plus intents 22 (`deploy_stack`) and 32 (capacity what-if), are seeded into a GitHub discussion to gather real demand rather than being designed speculatively.

## Testing

- **Row and digest shapes** — golden tests over a fixture cluster holding one resource of every type, so a Docker SDK field addition cannot silently leak into the wire format.
- **`reason` population** — a table test per cause (unmet constraint, image pull failure, OOM kill, rollout paused), each asserting the reason text, driven through the real cache.
- **Tier rule** — a test walking `toolCatalog()` and asserting every tool has exactly one tier and one target resource type, so a future merge cannot reintroduce a call-time tier.
- **Output schemas** — a test that marshals each tool's declared output schema and validates a real result against it. The `search` tool shipped a schema requiring `Hits` while emitting `results`; nothing caught it because no test compared the two.
- **Token budget** — a test asserting `find` over the fixture cluster stays under a byte ceiling, since the whole point is the size of the answer.

## Migration and Breaking Changes

`list_resources` → `find` is a breaking change for MCP clients, and the eight templated `cetacean://<type>/{id}` resources keep their URIs but change payload to the digest. There is no deprecation window: the MCP surface is pre-1.0, the compact shape is the entire point, and running both doubles the introspection cost the change exists to reduce. The `raw: true` escape hatch is what a client depending on Docker field names uses instead.

The eight merged tier-2 tools disappear from `tools/list`. The CHANGELOG entry must be explicit, as the `search` rename already is.

## Open Questions

None blocking. Tier assignments for the new fields are settled (secrets and mounts at 3, healthcheck and command at 2), the widget migrates in the same commit, and compound intents go to a discussion.
