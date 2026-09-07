---
title: MCP tools and resources
description: Reference for the resources, tools, prompts, and widgets Cetacean's MCP server exposes to an agent.
category: reference
tags: [mcp, ai, agents, tools, reference]
---

# MCP tools and resources

This page is the catalog an agent sees over Cetacean's MCP server: 12 resources, 27 tools, 6 prompts, and 5
widgets. For enabling the server, authorization, and configuration, see [MCP Server](mcp).

Everything here is filtered per identity. A tool above the configured operations level is not registered at
all; a tool or resource the identity holds no grant for is hidden from the listing and refused at call time.

## Resources

Resources are read-only views returned as `application/json`. Three are static, nine are URI templates.

| URI | Contents |
|---|---|
| `cetacean://cluster` | Node, service, task, and stack counts, node readiness, task counts by state, total and reserved CPU and memory, the largest node's capacity, converged and degraded service counts, last sync time |
| `cetacean://recommendations` | Current recommendation engine findings, each with severity, category, target resource, and rationale |
| `cetacean://history` | The most recent create, update, and delete events across every resource type, newest first, up to 100 |

Every numeric field in `cetacean://cluster` names its unit: `totalCPUCores`, `reservedCPUCores`,
`maxNodeCPUCores`, `totalMemoryBytes`, `reservedMemoryBytes`, `maxNodeMemoryBytes`. Both CPU figures are in
cores. `cetacean://history` names a task event after its service (`demo_flaky.3`) rather than repeating the
task ID; `resourceId` still carries the ID.

Templated resources return the same compact `Digest` the `describe` tool builds, so a subscription payload and
a tool result cannot describe one resource differently. `services/{id}/logs` is the exception: it is a raw log
stream.

| URI template | Contents |
|---|---|
| `cetacean://nodes/{id}` | Node digest, by ID or hostname |
| `cetacean://services/{id}` | Service digest, by ID or name, with cross-references |
| `cetacean://services/{id}/logs` | Service logs merged across replicas, subscribable; not a digest |
| `cetacean://tasks/{id}` | Task digest; the parent service and node are in `related`, not top-level fields |
| `cetacean://stacks/{name}` | Stack digest, rolling up its member resources |
| `cetacean://configs/{id}` | Config digest; the payload's size in bytes, never the payload |
| `cetacean://secrets/{id}` | Secret digest; the payload is never read, not even its length |
| `cetacean://networks/{id}` | Network digest, with the attached services |
| `cetacean://volumes/{name}` | Volume digest, keyed by name |

### Subscriptions

Clients call `resources/subscribe` with a URI. When the underlying cluster state changes, the server sends
`notifications/resources/updated` for that URI and the client re-reads.
`notifications/resources/list_changed` fires when resources are created or removed. Both are ACL-filtered per
notification: a client is only notified about resources its identity can read.

### Argument completion

Templated resource URIs and prompt arguments complete. A client editing `cetacean://services/{id}`, or a
prompt's `service` argument, calls `completion/complete` and receives the **names** of matching resources
rather than IDs, since a name is what a read resolves.

Matching is a case-insensitive substring, the same rule `find`'s `query` uses. Docker names carry their stack
as a prefix, so typing `prometheus` offers `monitoring_prometheus`. Completions are capped at 100 values, with
`total` and `hasMore` reporting what was left out. Completion reads the same ACL-filtered listing every other
read goes through, and a secret's payload is redacted on that path before a name is taken from it.

## Compact resource shapes

Tools and resource reads never return a raw Docker Engine object. Eight services as raw `swarm.Service` run to
roughly fourteen thousand tokens, mostly `Platforms` entries and a duplicated `PreviousSpec`, and the field a
caller wants (is this healthy?) is not in there, because state is derived from tasks. Every list and detail
read returns one of two shapes instead.

**Row** is one entry in a list, returned by `find`. Every row carries `id`, `name`, and `type` (singular:
`service`, `node`, `task`, and so on). `stack`, `state`, and `detail` (the most identifying secondary fact,
such as a service's image, a node's role, or a network's driver) are omitted where they do not apply.
`desired` and `running` are populated only where a replica count means something:

- services set both, the desired replica count and how many are running
- stacks set `desired` alone, counting member services rather than replicas
- nodes, tasks, configs, secrets, networks, and volumes set neither

**Digest** is the detail view of one resource, returned by `describe` and by every templated
`cetacean://<type>/{id}` read. Alongside `id`, `name`, `type`, and `state` it adds:

| Field | Contents |
|---|---|
| `reason` | The cause Swarm gave for a non-healthy `state`; omitted when the state is healthy |
| `since` | When the current state began: the oldest still-live failing task's timestamp, or the resource's own last-updated time |
| `details` | A type-specific map of facts, such as a service's image, replica counts, reserved CPU and memory, ports, and placement constraints, or a node's role and capacity |
| `related` | Always an array, even when empty: the resources this one references or is referenced by |
| `recentFailures` | Always an array: the task failures behind a failing state, newest first, capped at 5. Only a service digest populates it |
| `restarts` | A service's involuntary task terminations as `lastHour` and `lastWeek` counts. Services only |

`restarts` is the only field that reveals a restart loop. `state` reads `running` whenever a replica happens
to be up, and `recentFailures` drops tasks the orchestrator has already replaced, so a service crash-looping
every few seconds otherwise describes as healthy. `lastHour` answers whether it is failing now; the ratio
between the two windows answers whether the fault is new.

Every numeric field in `details` names its unit, such as `cpuLimitCores` (a float, in cores) and
`memoryLimitBytes`, or is reported as a duration string like `"10s"` (`healthcheckInterval`, an update
policy's `delay` and `monitor`). Docker's own types express these as unlabelled NanoCPU or nanosecond
integers. Environment variables are reported as `envNames`: names only, never values.

### The `raw: true` option

Both `find` (when `type` is given) and `describe` accept `raw: true`, adding the untouched Docker record to
the result under `raw`. Use it only when a specific field a row or digest omits is needed; the compact shapes
exist to avoid handing an agent a several-hundred-line object.

Raw records ride beside the compact shape rather than replacing it. `find` fills a `raw` array holding one
record per row it returned, and `describe` adds a `raw` object next to the digest's fields. A tool that
advertises an output schema must return structured content conforming to it whatever its arguments, so raw is
an addition to the declared shape. The compact half of a raw result is the same one a plain call returns,
filters and paging included.

## Tools

Tools are gated by operations level (`mcp.operations_level`, inheriting `operations_level`) and by
per-resource ACL write permission. Each tool advertises the behavioural hints (`readOnlyHint`,
`destructiveHint`, `idempotentHint`, `openWorldHint`) clients use to gate confirmation prompts, an output
schema the server validates results against, and an [icon](#icons). Results carry machine-readable
`structuredContent` alongside the text form. An input-validation failure comes back as a tool result with
`isError: true` so the model can self-correct, not as a protocol error. Mutating tools return `409` on a
Docker version conflict.

On connect the server sends top-level usage `instructions`: read-mostly model, resolve IDs with `find` first,
writes gated by operations level and ACL.

`find` and `describe` also attach `resource_link` content items for the resources their result is about, so a
host can offer somewhere to go next and a client can `resources/read` one without spelling a `cetacean://`
URI. Links describe the page returned, filters and paging included, and are capped at 25.

### Level 0: reads

Always available.

| Tool | What it does | Key arguments |
|---|---|---|
| `get_cluster_status` | Answers whether the cluster is healthy and, when it is not, names the services not in their desired state, the nodes down or draining, the rollouts in flight, and reserved against total CPU and memory. Start here | none |
| `find` | Locates resources. With `type` (plural) it enumerates that type, paged. Without `type` it searches every type at once | `type`, `query`, `state`, `stack`, `node`, `image`, `label`, `limit`, `offset`, `raw` |
| `describe` | Returns one resource as a `Digest` | `type` (singular, required), `id` (required), `raw` |
| `get_logs` | Fetches recent log lines from one scope | exactly one of `service`, `task`, `stack`, `cluster`; plus `contains`, `tail`, `since`, `level` |
| `get_events` | The change timeline, newest first | `since`, `until`, `types`, `resource`, `limit` |
| `get_topology` | Projects the cluster as a graph in three views | `view` (`network`, `placement`, `drain-impact`), `node` |
| `get_metrics` | Charts or ranks CPU, memory, or network use | `target` (required), `id`, `metric`, `range`, `top`, `by` |
| `get_recommendations` | The recommendation engine's current findings | `severity` |
| `watch` | Blocks until a service has settled, then reports whether it did | `service` (required), `timeout` |

`find`'s `type` is plural (`nodes`, `services`, `tasks`, `stacks`, `configs`, `secrets`, `networks`,
`volumes`); `describe`'s is singular. In a typed listing `limit` defaults to and is capped at 200, and
`offset` pages. In the cross-type search `limit` instead caps matches per type (default 3), `offset` is
ignored, and the result carries `counts`, the per-type breakdown behind `total`. `describe` accepts an ID or a
name: a hostname for a node, the rendered `<service>.<slot>` for a task, the plain name for everything else. A
name matching more than one resource is refused, naming the matching IDs.

`get_logs` takes exactly one scope. A service merges the output of its live replicas; a task reads one replica
and is the only way to reach one that has already exited, which makes it the form to reach for after a crash.
It is also the most perishable: Swarm keeps a task's output only while it keeps the task record, five per
replica slot by default (`--task-history-limit`), so a service restarting in a loop retains seconds of history
and should be read first. `stack` and `cluster` merge server-side and attribute every line to its service, so
grepping the cluster with `contains` costs one call. A wide read covers at most 25 services and reports any it
could not reach in `errors` rather than failing. `tail` defaults to 100 lines per replica.

`get_topology`'s `network` view joins services to the overlay networks they attach to, `placement` joins nodes
to the services they run, and `drain-impact` takes a `node` and joins the services running on it to the nodes
that could take them. In `drain-impact` a service with no edges is stranded, and its `detail` names the
placement constraint that blocked it. Global services are reported as `global`, since draining stops their
task rather than relocating it. Placement constraints are evaluated the way Swarm enforces them; spare
capacity is not considered, and `get_cluster_status` already reports reserved against total. The candidate
nodes are the ones the caller may read, so where grants hid any, the graph carries a `note` saying what the
assessment was narrowed to.

`get_metrics` takes a target and a metric rather than PromQL, resolves the service or node against Cetacean's
cache, and checks the caller's read grant before querying. `metric` is `cpu` (default), `memory`, or
`network`; `range` is `1h` (default), `6h`, `24h`, or `7d`. Asking for `top` turns it into a ranking:
`target: "cluster"` ranks the busiest services (or nodes, with `by: "node"`) and `target: "node"` with an `id`
ranks the services on that host, which answers why a node is hot. `top` defaults to 5 and is capped at 10.
With an ACL policy active, the caller's grants are compiled into the query rather than applied to its result.
Metrics need Prometheus, plus cAdvisor for service metrics and node-exporter for node metrics; without them
the tool reports that metrics are unavailable rather than returning empty series. The node scope matches on
Prometheus's standard `instance` label.

`get_recommendations` returns the same findings as `cetacean://recommendations`, with totals counting what the
caller may read. A finding with a remedy carries it as `fix`, naming the tool to call, for example
`{"tool": "update_node", "section": "availability"}`. A finding whose REST `fixAction` route has no MCP tool
behind it reports no `fix`.

`watch` exposes the same convergence rule the converging mutations use, as a read. `timeout` defaults to 60
seconds and is capped at 300; the wait cannot be cancelled once started.

### Level 1: operational

| Tool | What it does | Key arguments |
|---|---|---|
| `scale_service` | Sets the desired replica count. `0` stops every task without removing the service | `id`, `replicas` |
| `update_service_image` | Sets the container image, triggering a rolling deploy per the service's update policy | `id`, `image` |
| `rollback_service` | Reverts to the previous spec. Only one previous spec is retained, so calling twice does not unwind further | `id` |
| `restart_service` | Forces a rolling restart by bumping `ForceUpdate`. No other spec field changes | `id` |
| `remove_task` | Deletes a task; Swarm reschedules a replacement, so this is a forced reschedule | `id` |

The first four accept [task augmentation](mcp#tasks) and return a summary of where the service ended up
rather than its full spec:

```json
{"id":"web","name":"web","image":"nginx:1.27","mode":"replicated","replicas":5,"running":5,"state":"running","version":42}
```

`running` is the live count, `state` is the same derivation the dashboard and the API report, and `version` is
the Swarm version index for a caller doing its own concurrency checks. `replicas` is omitted for a global
service.

### Level 2: configuration

| Tool | What it does | Key arguments |
|---|---|---|
| `update_service` | Changes one section of a service's spec | `id`, `section`, `value` |
| `update_node_labels` | Patches a node's labels (JSON Merge Patch) | `id`, `labels` |
| `create_secret` | Creates a Swarm secret | `name`, `data`, `encoding`, `labels` |
| `create_config` | Creates a Swarm config | `name`, `data`, `encoding`, `labels` |
| `update_service_secrets` | Replaces the complete set of secrets a service receives | `id`, `secrets` |
| `update_service_configs` | Replaces the complete set of configs a service receives | `id`, `configs` |
| `update_service_mounts` | Replaces the complete set of mounts: named volumes, host bind mounts, tmpfs | `id`, `mounts` |

`update_service` takes one of ten sections in `section` and that section's new value in `value`:

| Section | Value | Semantics |
|---|---|---|
| `env` | Merge-patch object | A string sets a key, `null` deletes it, an omitted key is preserved |
| `labels` | Merge-patch object | As above; the only service section that does not trigger a rolling deploy, along with the two policies |
| `resources` | `ResourceRequirements` | CPU and memory limits and reservations |
| `placement` | `Placement` | Constraints, preferences, max replicas per node, platforms; may reschedule tasks |
| `ports` | Array of `PortConfig` | Drops connections to any port it removes or remaps |
| `update-policy` | `UpdateConfig` | Applies to the next spec change |
| `rollback-policy` | `UpdateConfig` | Applies to `rollback_service` |
| `log-driver` | `Driver` | Name plus options; routes subsequent log lines |
| `healthcheck` | Probe | `test` as an array (`["CMD-SHELL", "curl -f localhost \|\| exit 1"]`), plus `interval`, `timeout`, `startPeriod`, `retries`. Durations are strings such as `"10s"`. An empty `test` removes the check |
| `command` | `command` plus `args` | The entrypoint and what follows it, the split Docker makes. Omitting one clears it |

Every section other than `env` and `labels` replaces its section wholesale: a field you omit is cleared.
Because `value` is whatever the section takes, the input schema cannot describe it, and the section's decoder
validates it instead, naming the section and the shape it wanted.

Secrets, configs, and mounts are not sections of `update_service`. Each takes a list rather than a spec
fragment, and attaching a secret checks a read grant on the secret as well as a write grant on the service,
so each is a tool of its own. Passing one as a `section` is refused with the name of the tool to use.

All three replace their set wholesale: pass every entry the service should end up with, because one left out
is detached, and a container may lose data it was writing to a dropped mount.

> **Note:** a bind mount hands the container the host's filesystem at that path, and binding
> `/var/run/docker.sock` gives it control of the whole cluster. `update_service_mounts` will do it if asked;
> the operations level is not what stops it.

Swarm secrets and configs are immutable, so rotating one is three calls in order: `create_secret` for the
replacement, `update_service_secrets` to repoint each service that uses it (`describe` the secret first, its
`related` array names them), then `remove_secret` once nothing references it. Configs follow the same
sequence, and unlike a secret a config's content can be read back, so use a secret for anything sensitive.

The spec-editing tools return the section they changed rather than the whole service:

```json
{"id":"web","name":"web","version":42,"section":"resources",
 "details":{"cpuLimitCores":2,"memoryLimitBytes":1073741824}}
```

`details` is the same projection `describe` builds, narrowed to the edited section, so confirming an edit and
describing the resource afterwards cannot disagree. It reports `envNames` and a log driver's `optionNames`,
never their values, so a call that raises a CPU limit does not return the service's credentials. The node
tools answer with `id`, `hostname`, `version`, `section`, and `details`, plus `role` and `availability` on
every one of them.

### Level 3: impactful

| Tool | What it does | Key arguments |
|---|---|---|
| `update_node` | Changes a node's `availability` (`active`, `pause`, `drain`) or `role` (`worker`, `manager`) | `id`, `section`, `value` |
| `remove_service` | Deletes a service and stops every task. Does not delete the volumes, networks, configs, or secrets it referenced | `id` |
| `remove_config` | Deletes a config. Fails if any service still references it | `id` |
| `remove_secret` | Deletes a secret. Fails if any service still references it | `id` |
| `remove_network` | Deletes an overlay network. Fails if a service is attached; `ingress` and `docker_gwbridge` cannot be removed | `id` |
| `remove_volume` | Deletes a volume by name. Fails if a container is using it unless `force` is set | `name`, `force` |

Removals are irreversible: no spec history is retained after a service removal, and a config, secret, or
volume payload cannot be recovered.

Draining a manager may destabilise the swarm, so prefer draining workers. Promoting a node adds a Raft voter
and demoting removes one: never reduce the manager count below the quorum threshold, and on a three-manager
cluster demote at most one at a time. Node labels stay a tool of their own, `update_node_labels`, at level 2,
so a deployment that lets an agent relabel a node does not also let it demote a manager.

Every MCP tool requires the same operations level as the API route for the same operation, so there is no
separate MCP level table to keep in step.

## Prompts

Prompts are named sequences a client offers from a menu. Picking one seeds the conversation with an
investigation or a runbook.

| Prompt | Level | Argument | Reads | What it does |
|---|---|---|---|---|
| `diagnose_service` | 0 | `service` | service | Walks tasks, the failing task's logs, metrics, and recent changes to find why a service is unhealthy |
| `explain_unschedulable` | 0 | `service` | service, node | Separates the causes of an unplaced task: placement constraints, node labels and platform, node availability and state, per-node replica caps, reservations |
| `review_capacity` | 0 | none | node | Joins node capacity, reservations, real usage, and sizing findings to say where the cluster is constrained |
| `roll_back_service` | 1 | `service` | service | Confirms a service is degraded, then rolls it back and waits for the replicas to run |
| `right_size_service` | 2 | `service` | service | Checks a sizing recommendation against measured use, then corrects the reservations |
| `drain_node` | 3 | `node` | node, service | Checks quorum and that the work can be placed elsewhere, then drains the node and confirms the tasks moved |

A prompt's level is the highest level of the tools it walks, so it is never offered where one of its steps
would be refused. At the default level 1 you get the three diagnostic prompts plus `roll_back_service`.

Prompts are also filtered by ACL, all or nothing: a prompt is offered only when every tool it walks is
available to you and you hold read on every resource type in its Reads column. The read-type check matters
because the cross-type reads (`find`, `describe`, `get_events`, `get_cluster_status`, `get_topology`,
`get_metrics`, `get_recommendations`) are ungated, each ACL-filtering its own results, so a sequence built
only from those would otherwise be offered to someone who gets an empty list from every step. A caller whose
grants match nothing is offered no prompts. A prompt you cannot see reports `not found` from `prompts/get`.

Where a composite read exists, the sequence calls it rather than describing the join: `drain_node` uses
`get_topology`'s drain-impact view, `review_capacity` opens on `get_cluster_status` and ranks nodes with one
`get_metrics` call, the two prompts that need a change history call `get_events` narrowed to their service,
and every step that waits calls `watch`.

A prompt expands to a single message with the resource name you supplied. It reads no cluster data and does
not check that the name exists; the text tells the model to resolve it with `find` first. Every read and write
the prompt describes goes through the normal tool and resource paths, including ACL checks.

The six prompts cover the sequences that recur most and cost the most tool calls unaided.

## Widgets

A host that supports the MCP Apps extension can render Cetacean's data as an interactive view instead of JSON.
Cetacean advertises `io.modelcontextprotocol/ui` and serves each widget as a resource.

| Resource | Renders | Behaviour |
|---|---|---|
| `ui://cetacean/table` | A `find` result | Searchable, sortable table of one resource type, showing how many records it holds when the page is a subset |
| `ui://cetacean/topology` | A `get_topology` result | Graph to pan, zoom, and drag; switching view re-runs the tool under the same identity and grants |
| `ui://cetacean/logs` | A `get_logs` result | Live tail, re-reading from the cursor the previous read returned; level and search filtering happens over the lines already fetched |
| `ui://cetacean/metrics` | A `get_metrics` result | Line chart with a range picker; changing the range re-runs the tool. Every series is named in a legend with its latest value |
| `ui://cetacean/recommendations` | A `get_recommendations` result | Findings grouped by severity, most serious first. Picking one asks the host to send the model a follow-up, which a host may decline |

Each tool names its widget in `_meta`, so a host knows which view fits the result. The same tool called from a
client without app support returns JSON.

Each widget is a single self-contained HTML document with MIME type `text/html;profile=mcp-app`, all CSS and
JavaScript inlined, because an app resource has no base URL and cannot fetch anything relative to itself.
Widgets read data by calling Cetacean's MCP tools through the host, never Cetacean's HTTP API directly, so
every read stays on the audited path and a widget sees exactly what the calling identity's grants allow.

Each widget declares an empty `_meta.ui.csp`, stating that it needs no external origin: no network, no
third-party assets, no nested frames.

Widgets are optional in both directions. A host without app support ignores the extension and receives
ordinary results, and a binary built without `npm run build:widgets` serves no widget resources and does not
advertise the extension.

## Icons

Every tool and resource advertises an `icon` that MCP clients can render beside it. Tool icons are grouped by
verb category (read, search, scale, edit, node, remove); resource icons reflect the resource type. The server
itself carries a display `title`, a `description`, a `websiteUrl`, and an icon, so a host listing several MCP
servers shows Cetacean by name rather than by its programmatic ID.

The icons are plain SVGs served under the unauthenticated `/assets/mcp-icons/` prefix, so a client loads them
without a bearer token in every auth mode. Their URLs are absolute and derived from the canonical external
base URL, so set `mcp.issuer` when Cetacean runs behind a reverse proxy or the icon URLs will point at the
wrong host. If no external base URL can be resolved, icons are omitted rather than advertised as broken
relative links.
