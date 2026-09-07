# MCP Evaluation Set

What people actually ask their agent about a Swarm cluster, grouped by the **moment they are in**
rather than by resource type.

That grouping is load-bearing. Sorted by resource, a corpus like this reads as adequate coverage —
there is a tool for services, one for nodes, one for logs. Sorted by moment, it exposes whether the
surface is shaped like the Docker API or like a question. Most of what was missing showed up in the
gaps between resources: "what broke overnight", "grep the cluster", "if I drain this node, what
moves".

This file has two jobs.

1. **Evaluate.** Run these against a live cluster and judge the answers. `docs/test_protocol.md` is
   the same idea for the dashboard; this is the agent surface.
2. **Design.** Every ask that cannot be answered cleanly is a missing tool, a missing argument, or a
   missing prompt. Demand comes first and the surface follows it, rather than the tool list deciding
   what is askable.

Entries 1–67 are the original corpus and keep their original numbers permanently, so the coverage
markers can be compared across releases. Later additions start at 68. The markers below were
re-derived against the current build — see [What changed](#what-changed) for the delta.

## Legend

| | Meaning |
|---|---|
| ✓ | Answerable, first time, in one or two calls |
| ~ | Only obliquely or expensively — several calls, manual joining, or a partial answer |
| ✗ | Not answerable |

Two rules when judging:

- **An answer that reads as complete but is not is worse than an error.** The agent may act with no
  human watching, and "no matches" is indistinguishable from "I only looked at a quarter of it"
  unless the payload says so.
- **Count the round-trips.** Most of these should be one or two calls. Five is a design note even
  when the final answer is right.

Ask each question the way it is written, vague bits included. If it only works once rewritten into
Cetacean's vocabulary, that is the finding — record the phrasing that failed.

---

## 1. Landing — "what's the situation?"

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 1 | Is the cluster healthy? | ✓ | Names what is wrong and where to look, not a count | `get_cluster_status` |
| 2 | Are there any services with problems right now? | ✓ | The services, with state and replica counts | `get_cluster_status` |
| 3 | What broke overnight / since I last looked? | ✓ | Changes and failures in that window, newest first | `get_events` with `since` |
| 4 | Is anything mid-rollout? | ✓ | Rollouts in flight, and any Swarm has paused | `get_cluster_status` |
| 5 | Are any nodes down or draining? | ✓ | Unreachable nodes separated from deliberately drained ones | `get_cluster_status` |
| 6 | Are we near capacity anywhere? | ✓ | Reserved against total in named units, both CPU and memory | `get_cluster_status` |
| 7 | Give me the morning summary. | ✓ | Health, overnight changes, and anything still degraded | `get_cluster_status` → `get_events` |

---

## 2. Triage — "why is this one broken?"

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 8 | Why is service X failing? | ✓ | The failing task, its reason, and the output that explains it | `describe` → `get_logs` |
| 9 | Why won't X schedule? | ✓ | The constraint, capacity or image reason Swarm refused it | `explain_unschedulable` |
| 10 | Why is X restarting in a loop? | ✓ | Failure counts over an hour and a week, plus the crash output | `describe` → `get_logs` |
| 11 | Which replica is the broken one? | ✓ | The specific task, addressable for a follow-up | `find` type `tasks` |
| 12 | Is this the app or the infrastructure? | ~ | Requires joining node health against service output by hand | `describe` → `get_logs` |
| 13 | What's the actual error — not the state, the reason? | ✓ | The reason behind the state, carried on the digest | `describe` |
| 14 | Has X always been like this, or is it new? | ✓ | Failures over both windows, so new is distinguishable from chronic | `describe` |

---

## 3. Deploy & verify

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 15 | Has some-app deployed yet? | ✓ | Whether the cluster caught up, not whether Docker accepted | `watch` |
| 16 | Is the rollout finished / converged? | ✓ | Converged or how far it got | `watch` |
| 17 | Deploy image v2 to X and tell me when it's live. | ✓ | The new image, then confirmation the replicas are on it | `update_service_image` → `watch` |
| 18 | Watch X until it deploys correctly, or tell me if it fails. | ✓ | A blocking wait with the last progress line on timeout | `watch` |
| 19 | What image is X running right now? | ✓ | Image and tag, and whether every replica is on it | `describe` |
| 20 | Roll X back. Did the rollback work? | ✓ | What it rolled back *to*, and that it settled | `roll_back_service` |
| 21 | What changed between the current and previous spec of X? | ✗ | A field-level diff. History records that it changed, not what | — |
| 22 | Deploy this stack. | ✗ | No stack-level deploy exists; services are changed one at a time | — |

---

## 4. Logs

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 23 | another-app is showing errors — what are they? | ✓ | The matching lines, narrowed server-side | `get_logs` with `level` |
| 24 | Anything in the logs in the last five minutes? (cluster-wide) | ✓ | Matches from across the cluster, and how far it looked | `get_logs` with `cluster` |
| 25 | Show me every error across the cluster in the last hour. | ✓ | As above, narrowed by level and time | `get_logs` with `cluster` |
| 26 | Grep the cluster for "connection refused". | ✓ | Matches only, attributed per service — not every line | `get_logs` with `contains` |
| 27 | Tail X while I reproduce this. | ~ | No streaming tool; repeated reads from the returned cursor | `get_logs` with `since` |
| 28 | Show me the logs of the container that just died. | ✓ | The dead replica's output, or a clear reason it is gone for good | `get_logs` on a task |
| 29 | This started at 14:02 — what else happened around then? | ~ | Two calls sharing a timestamp format but not a shape; merged by hand | `get_events` + `get_logs` |

---

## 5. Capacity, performance, cost

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 30 | What's using the most CPU / memory? | ✓ | A ranked list, named, with values | `get_metrics` |
| 31 | Is X over- or under-provisioned? | ✓ | Measured usage against reservation, or a refusal if unmeasured | `right_size_service` |
| 32 | Can I fit five more replicas of X? | ~ | Reservation and headroom are both available; the arithmetic is manual | `describe` → `get_cluster_status` |
| 33 | Why is node N hot? | ✓ | The services on *that node*, ranked | `get_metrics` scoped to a node |
| 34 | Are any volumes filling up? | ✗ | Metrics cover CPU, memory and network only | — |
| 35 | Right-size X and apply it. | ✓ | The proposal, the evidence, then the applied limits | `right_size_service` |
| 74 | Chart X's memory for the last day. | ✓ | A series over the requested range, not an instant value | `get_metrics` |

---

## 6. Relationships & blast radius

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 36 | Which services can reach each other? | ✓ | Services joined by the overlays they share, `dnsrr` included | `get_topology` network |
| 37 | What runs where? | ✓ | Nodes joined to the services they actually run | `get_topology` placement |
| 38 | Who uses secret Y / config Z? | ✓ | Every service referencing it | `describe` on the secret |
| 39 | If I drain node N, what moves? | ✓ | What moves, where to, and what is stranded with the reason | `get_topology` drain-impact |
| 40 | What's in stack S? | ✓ | Member services with state | `describe` on the stack |
| 41 | What's exposed publicly? | ~ | No filter by published port; means listing and inspecting each | `find` → `describe` |
| 77 | Are replicas spread evenly? | ✓ | The distribution, and where it is lopsided | `get_recommendations` |

---

## 7. Inventory & audit

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 42 | What env vars does X have? | ✓ | The names, never the values | `describe` |
| 43 | What are X's limits, constraints, ports, mounts? | ✓ | All of them, in named units | `describe` |
| 44 | Which secrets and configs exist? Which are orphaned? | ~ | Listing is one call; orphan detection is one `describe` per secret | `find` → `describe` |
| 45 | What images are we running? Anything on `:latest`? | ~ | `find` filters by image substring; no aggregate view | `find` with `image` |
| 46 | Which services have no healthcheck? | ✓ | The services, and the tool that would add one | `get_recommendations` |
| 47 | What labels are on node N? | ✓ | The labels | `describe` |
| 68 | What secrets and configs does X receive? | ✓ | Names and mount paths, never contents | `describe` |
| 69 | What happens when X fails to update? | ✓ | Its update and rollback policy | `describe` |
| 70 | Where do X's logs go? | ✓ | The driver and its option *names* | `describe` |
| 73 | Which services mount the Docker socket or run privileged? | ~ | Requires pulling raw records and reading them | `find` with `raw` |
| 75 | What's running on a single replica? | ✓ | The services, and which look load-bearing | `get_recommendations` |
| 76 | Is anything running on the managers that shouldn't be? | ✓ | The workloads on manager nodes | `get_recommendations` |
| 78 | Which services are flaky? | ✓ | Services by involuntary failure count over a real window | `get_recommendations` |
| 79 | Find the service handling checkout. | ✓ | Matches by name, image and label — not name alone | `find` with `query` |

---

## 8. Routine mutations

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 48 | Scale X to 5. / Restart X. / Update X to v2. / Roll X back. | ✓ | A compact confirmation with the resulting state and version | `scale_service` and friends |
| 49 | Remove the failed tasks. | ✓ | Confirmation per task | `remove_task` |
| 50 | Drain node N for maintenance, then bring it back. | ✓ | The impact assessment *before* the drain, then the drain | `drain_node` → `update_node` |
| 51 | Promote / demote a node. | ✓ | The resulting role | `update_node` section `role` |
| 52 | Label node N so demo_stuck can schedule. | ✓ | The resulting labels, and that the task then placed | `update_node_labels` → `watch` |

---

## 9. Configuration changes

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 53 | Update the secret of foo to X. | ✓ | Swarm secrets are immutable, so: create, repoint, remove | `create_secret` → `update_service_secrets` → `remove_secret` |
| 54 | Add an env var to X. | ✓ | The resulting variable names | `update_service` section `env` |
| 55 | Raise X's memory limit. | ✓ | The resulting limits in named units | `update_service` section `resources` |
| 56 | Add a healthcheck to X. | ✓ | Probe, interval, timeout, start period, retries | `update_service` section `healthcheck` |
| 57 | Change X's command / args. | ✓ | Entrypoint and arguments, kept distinct | `update_service` section `command` |
| 58 | Mount a volume into X. | ✓ | The resulting mount list, replaced not merged | `update_service_mounts` |
| 59 | Publish a port. | ✓ | The resulting port list | `update_service` section `ports` |

---

## 10. Compound / guarded

The multi-step, partly destructive asks. Every step exists; what is missing is the sequence, the
ordering guarantee and the confirmation between steps.

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 60 | Rotate the DB password everywhere it's used. | ~ | Every step exists; nothing sequences them or guards the ordering | `describe` → create → repoint → remove |
| 61 | Restart every service in stack S. | ~ | No bulk operation; means one call per member service | `find` → `restart_service` ×N |
| 62 | Scale the dev stack to zero overnight. | ~ | Same fan-out, and nothing schedules it for later | `find` → `scale_service` ×N |
| 63 | Fix all the missing healthchecks. | ~ | The finding is one call; the fix is one call per service | `get_recommendations` → `update_service` ×N |
| 64 | Apply that recommendation. | ✓ | Findings name the tool that applies them | `get_recommendations` → named tool |

---

## 11. Meta

| # | The ask | ? | A good answer | Path |
|---|---|---|---|---|
| 65 | What can you see? What am I allowed to change? | ~ | The tool list is already filtered, so it answers implicitly; nothing states the operations level or grants | — |
| 66 | Why was that refused? | ~ | The error explains, but not in a form that separates "not permitted" from "not possible" | — |
| 67 | Explain this recommendation. | ✓ | The finding carries its severity, subject and reasoning | `get_recommendations` |
| 71 | Show me the value of secret Y. | ✓ | A refusal. The value is write-only and no tool returns it | `describe` |
| 72 | Show me a service I have no grant for. | ✓ | Not found or refused, with no partial disclosure through a related resource | `describe` |

---

## What changed

The original markers were taken before the 0.13.0 agent surface landed. Re-deriving them shows what
that release actually bought — and, more usefully, what it did not.

**Was ✗, now ✓ — 13.** 6 near capacity, 18 watch until deployed, 24/25/26 cluster-wide logs and
grep, 28 a dead replica's output, 30 what's using the most, 38 who uses a secret, 39 drain impact,
53 update a secret, 56 add a healthcheck, 57 change command, 58 mount a volume.

**Was ~, now ✓ — 15.** Landing but for capacity (1–5, 7), 9 unschedulable, 13 the reason not the
state, 14 new or chronic, 15/16 has it deployed, 35 right-size, 40 stack contents, 46 missing
healthchecks, 64 apply a recommendation.

**Was ✗, now ~ — 8.** 27 live tail, 29 correlate at 14:02, 32 will it fit, 41 published ports,
44 orphaned secrets, 45 image inventory, 60 rotate everywhere, 63 fix all healthchecks. The pieces
exist; nothing joins them.

**Still ✗ — 3.** 21 spec diff, 22 stack deploy, 34 volume fill.

**Re-assessed, not regressed — 2.** 12 app-or-infrastructure and 65 what am I allowed to change were
unmarked in the original. Both are marked ~ here: neither has a single call that answers it, and
scoring them as working hid that.

The shape of what remains is the point. Of 17 asks still short, only four want a genuinely new
*read* (21, 34, 41, 73). The rest are **composition** — section 10 entire, plus 44, 45, 63 — and
**self-description**, 65 and 66. The surface can now answer questions about the cluster. It is still
weak at carrying out a sequence, and at describing itself.

## Still open

Grouped by what it would take, rather than by section.

- **Needs a prompt, not a tool** — 60 rotate everywhere, 63 fix all healthchecks, 29 correlate a
  moment. Every underlying call already exists.
- **Needs a bulk or fan-out argument** — 61 restart a stack, 62 scale a stack. One call per member
  service is the whole cost.
- **Needs a new read** — 21 spec diff, 34 volume fill, 41 published ports, 73 socket mounts and
  privileged services.
- **Needs an aggregate over an existing read** — 44 orphaned secrets, 45 image inventory. Both are
  answerable one resource at a time, which is the problem.
- **Needs self-description** — 65 what am I allowed to change, 66 why was that refused. An agent
  discovers its limits by being refused, and cannot tell a permission failure from an impossibility.
- **Inherently a judgement, not a lookup** — 12 app or infrastructure, 32 will it fit. Worth watching
  rather than fixing: if a prompt makes either reliable, that is the answer.
- **No streaming** — 27 live tail. The transport is request/response; repeated reads from the
  returned cursor are the closest thing, and are what the logs widget does.
- **Out of scope for now** — 22 stack deploy. A compose-file operation, not a cluster query.

## Cross-cutting expectations

Not questions. They apply to every answer above, and most disappointment comes from here rather than
from a missing feature.

1. **Follow-ups keep their subject.** "And its logs?" after a `describe` must not need the service
   re-identified. Every answer carries the identifiers the next call needs.
2. **Vague asks resolve.** "Something's wrong with the api" lands on the right service rather than
   bouncing back for an ID.
3. **Names work as well as IDs.** People type `web`, not `9308tz5be1qt`. An ambiguous name is
   refused with the candidates named, never silently resolved to one of them.
4. **Bounds are visible in the payload.** Every cap, window and truncation is stated in the result,
   not only in the tool description — the result is the half the model reasons over.
5. **Units are named.** `cpuLimitCores`, `memoryLimitBytes`. Never two figures in different units
   under names that mention neither.
6. **Refusals explain.** "Metrics unavailable, Prometheus is not configured" beats an empty chart.
7. **One question, one call.** If a common ask reliably costs three round-trips, that is a prompt or
   an argument waiting to be added.

## Prompt coverage

| Prompt | Serves |
|---|---|
| `diagnose_service` | 8, 10, 13, 14, 23 |
| `explain_unschedulable` | 9 |
| `review_capacity` | 1, 6, 30 |
| `right_size_service` | 31, 35 |
| `roll_back_service` | 20 |
| `drain_node` | 39, 50 |

Unguided clusters worth a prompt, ranked by how often the asks recur and how many calls they cost
unaided:

1. **Secret rotation** — 53, 60, 38. Four steps, a destructive last one, and an ordering that
   matters. The strongest candidate by some distance.
2. **Incident triage** — 2, 3, 7, 24, 25. The on-call entry point, currently three or four separate
   calls every time.
3. **Deploy and verify** — 15, 16, 17, 18, 20. Push, wait, confirm, roll back if it did not take.

## Extending this file

Add to it when a real ask goes badly. The valuable entries are the ones nobody predicted, so record
them verbatim — the phrasing that failed *is* the finding, and rewriting it into working vocabulary
destroys the evidence. Note which tools it took, how many calls, and what the answer got wrong.

Keep existing numbers stable. Re-derive the markers each release rather than editing them in place,
so the delta stays readable. An entry that goes from ✓ back to ~ is a regression this set exists to
catch.
