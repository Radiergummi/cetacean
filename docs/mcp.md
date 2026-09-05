---
title: MCP Server
description: Embedded Model Context Protocol server exposing cluster state and write operations to AI agents over streamable HTTP, with OAuth 2.1 authorization.
category: guide
tags: [mcp, ai, agents, oauth, automation]
---

# MCP Server

Cetacean embeds a [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that turns the dashboard
into an interface AI agents can reason over. Agents read the same real-time cluster state Cetacean shows in the
browser — nodes, services, tasks, stacks, configs, secrets, networks, volumes, recommendations, change history — and
make the same safe, ACL-gated changes a human operator could make through the UI.

The server is disabled by default. Enable it with `CETACEAN_MCP=true`. Everything it exposes is governed by the same
operations level, authentication, and ACL policy as the REST API — MCP is a second transport over the existing
authorization model, not a new privilege path.

## Quick start

```bash
CETACEAN_MCP=true \
CETACEAN_AUTH_MODE=oidc \
CETACEAN_MCP_ISSUER=https://cetacean.example.com \
  ./cetacean
```

The MCP endpoint is served at `{base_path}/mcp`. Point an MCP-capable client (Claude Code, Cursor, etc.) at it:

```jsonc
// Claude Code: .mcp.json
{
  "mcpServers": {
    "cetacean": {
      "type": "http",
      "url": "https://cetacean.example.com/mcp"
    }
  }
}
```

On first connect the client is challenged for authorization, walks the OAuth discovery chain, and prompts the
operator to sign in and consent. No client secret or manual registration is required.

## Protocol version and compatibility

The server speaks MCP **streamable HTTP** at revision **`2026-07-28`**, and only that revision. Older revisions are
refused with an `unsupported protocol version` JSON-RPC error naming the version to use, so an out-of-date client
fails immediately and legibly instead of connecting and then quietly receiving nothing. The deprecated HTTP+SSE and
stdio transports are not supported.

There are no sessions. `2026-07-28` removed the `initialize` handshake and `Mcp-Session-Id` along with it: every
request carries its own protocol version, client identity, and capabilities in `_meta`, and is served on its own.
This is why there is nothing to reconnect to and no session state to size — a client simply issues its next request.
Bearer tokens are valid on any replica that shares the signing key, so a request may be served by any of them.

A client receives server-initiated notifications by opening a `subscriptions/listen` stream, which replaces both
`resources/subscribe` and the old standalone `GET` stream. Notification types are opt-in: the stream delivers only
what the client's filter asked for.

Cetacean is pre-1.0 and the MCP server shipped days before this revision, so nothing was gained by carrying the
older eras forward. If you are on an older client, upgrade it.

## Authentication and authorization

### When auth mode is `none`

The OAuth endpoints are not registered and `/mcp` is unauthenticated. Anyone who can reach the endpoint has whatever
access the operations level allows. Only appropriate for trusted networks.

### OAuth 2.1 (auth modes `oidc`, `tailscale`, `headers`)

When MCP is enabled and the auth mode supports a browser flow, Cetacean acts as an OAuth 2.1 authorization server
**and** identifies itself as a protected resource for `/mcp`. It implements the MCP `2026-07-28` authorization
profile:

| Endpoint | Purpose |
|---|---|
| `GET {base}/.well-known/oauth-protected-resource` | Protected Resource Metadata (RFC 9728) — advertises the AS |
| `GET {base}/.well-known/oauth-authorization-server` | Authorization Server Metadata (RFC 8414) |
| `GET {base}/.well-known/openid-configuration` | Same metadata at the OpenID Connect Discovery 1.0 location (for clients that discover the AS that way) |
| `GET {base}/oauth/authorize` | Authorization endpoint (renders the consent screen) |
| `POST {base}/oauth/token` | Token endpoint |
| `POST {base}/oauth/revoke` | Token revocation (RFC 7009) |
| `POST {base}/oauth/register` | Dynamic Client Registration (RFC 7591) |

The flow:

1. The client hits `/mcp` without a valid token and receives `401` with a `WWW-Authenticate` header pointing at the
   Protected Resource Metadata document.
2. The client discovers the authorization server, then identifies itself by one of:
   - **Client ID Metadata Documents (CIMD)** — *recommended.* The `client_id` is an `https://` URL pointing at a
     published metadata document. Cetacean fetches and verifies it (with SSRF protections), and the consent screen
     shows a "verified via published metadata" badge, because the client's identity was checked against something it
     does not control at consent time. `2026-07-28` prefers CIMD and deprecates DCR. Advertised as
     `client_id_metadata_document_supported` in the AS metadata, and switchable with `CETACEAN_MCP_CIMD_ENABLED` —
     turning it off stops Cetacean making any outbound fetch on a client's behalf, at the cost of refusing every
     `https://` client_id.
   - **Dynamic Client Registration (RFC 7591)** — supported for backwards compatibility; **deprecated in MCP
     `2026-07-28`**. POST to `/oauth/register`. Every DCR client is public and PKCE-only; symmetric
     (`client_secret`) auth methods are rejected. A DCR client names itself, so the consent screen shows a
     "self-reported identity" badge. Registration is in memory, so registrations are lost on restart. Still enabled
     by default (`CETACEAN_MCP_DCR_ENABLED`); prefer CIMD for anything new.
3. The operator is authenticated by the configured auth provider, sees a consent screen, and approves.
4. The client exchanges the authorization code (PKCE-S256, single-use, 60s) for an access token and refresh token.
5. Subsequent MCP requests carry `Authorization: Bearer <token>`.

**Tokens.** Access tokens are HS256 JWTs scoped to this instance via an `aud` claim equal to the canonical `/mcp`
URL — a token minted for one Cetacean deployment cannot be replayed against another. Resource indicators (RFC 8707)
are required by default. Refresh tokens are opaque, rotate single-use, and carry an absolute grant-family lifetime;
presenting a rotated token revokes the whole family (theft detection).

### Remembered approvals

Approving a client is remembered, so you are asked once rather than every time
its refresh token expires. A record ties your identity to that client, the MCP
endpoint it asked for, and a fingerprint of the client's name and redirect URIs
as you were shown them.

You are asked again when any of those change — including when a client updates
its own metadata document, since a client identified by URL controls what that
document says — and when the grant is revoked or Cetacean detects a stolen
refresh token. Clients that registered dynamically are never remembered: their
metadata is self-reported.

An approval is also a lease, not a permanent grant. It lasts
`CETACEAN_MCP_CONSENT_TTL` (default `2160h`, 90 days) from the moment you
approved, and approving again renews it. The lease exists because revoking an
approval means presenting a token from its grant family, and that family is torn
down once its refresh token expires — so without one, an approval belonging to a
long-unused client would keep authorizing silently with no handle left to
withdraw it. The default deliberately outlives the 30-day refresh token, since
not re-prompting when that token expires is the whole point of remembering.

Set `CETACEAN_MCP_CONSENT_TTL=0` to turn remembering off entirely: nothing is
recorded, existing records stop being honoured, and every authorization reaches
a human.

Approvals live in `mcp-tokens.json` beside the refresh tokens. Note the file's
threat model differs between the two: refresh tokens are stored as SHA-256
hashes, which are useless to anyone who steals the file, while an approval is a
capability — anyone able to *write* the file could pre-approve a client. The
file is mode `0600`, and as always an attacker with host access has already won.

### `cert` mode and `CETACEAN_MCP_AUTH_BYPASS`

mTLS client-certificate auth can't drive a browser consent flow, so OAuth is not used in `cert` mode. Set
`CETACEAN_MCP_AUTH_BYPASS=cert` to let mTLS-authenticated clients reach `/mcp` directly with their client
certificate, deriving identity from the cert and skipping the bearer-token requirement.

### ACL enforcement

Every resource read, tool call, and notification is checked against the ACL policy for the request's identity —
nothing is cached per session, so policy hot-reloads take effect immediately. `tools/list` is filtered per identity:
an operator with read-only grants sees only the read tools. Resource reads return identical `not found` errors
whether a resource is absent or merely denied, so the policy doesn't leak existence. See
[Authorization](authorization.md) for the grant model.

Filtering a catalog is a coarser question than authorizing a call: a listing asks "could this identity ever act on a
service?", where a call names one. Cetacean answers the coarse question by projecting grants onto resource *types*,
expanding them exactly as a call does — a `stack:X` grant reaches the services, tasks, configs, secrets, networks and
volumes in that stack, a `service:X` grant reaches that service's tasks, and `write` implies `read`. So a stack-scoped
operator is shown the service tools their grant covers. The projection is an over-approximation in one direction only,
the same one a pattern already is: `service:web-*` reports the service type whether or not a matching service exists.
It never widens what a call may do — every listed tool is still checked against the named resource when invoked.

## Resources

Resources are read-only views, all returned as `application/json`.

**Static** (`resources/list`) mirror the REST API's cache-backed responses directly:

| URI | Description |
|---|---|
| `cetacean://cluster` | Swarm status, managers, raft, CA config |
| `cetacean://recommendations` | Current recommendation engine findings |
| `cetacean://history` | Recent resource change events |

Two of the three are projected rather than served verbatim, because the cache's own shape would mislead a reader
who has no dashboard code to compensate for it:

- `cetacean://cluster` names the unit in every numeric field — `totalCPUCores`, `reservedCPUCores`,
  `maxNodeCPUCores`, `totalMemoryBytes`, `reservedMemoryBytes`, `maxNodeMemoryBytes` — and reports both CPU
  figures in cores. The REST response behind it holds total CPU in cores and reserved CPU in nanoCPUs under
  unlabelled names, so dividing one by the other as spelled is wrong by nine orders of magnitude.
- `cetacean://history` names a task event after its service (`demo_flaky.3`) instead of repeating the task's own
  ID, which is what the cache records. `resourceId` still carries the ID. Without this a history read of a
  restarting service is a wall of opaque identifiers, and the one resource meant to answer "what changed?"
  cannot say what changed.

**Templated** (`resources/templates/list`) return the same compact `Digest` the `describe` tool builds — a resource
read and a `describe` call go through the same function, so a subscription payload and a tool result can never
describe the same resource differently. The one exception is `services/{id}/logs`, a raw log stream rather than a
digest.

| URI template | Description |
|---|---|
| `cetacean://nodes/{id}` | Node digest |
| `cetacean://services/{id}` | Service digest with cross-references |
| `cetacean://services/{id}/logs` | Service logs, merged across replicas (subscribable; not a digest) |
| `cetacean://tasks/{id}` | Task digest — its parent service and node are reachable via `related`, not as top-level fields |
| `cetacean://stacks/{name}` | Stack digest, rolling up its member resources |
| `cetacean://configs/{id}` | Config digest — payload size in bytes, never the payload; call `describe` with `raw: true` for the base64 data |
| `cetacean://secrets/{id}` | Secret digest (payload never read, not even its length) |
| `cetacean://networks/{id}` | Network digest |
| `cetacean://volumes/{name}` | Volume digest |

See [Compact resource shapes](#compact-resource-shapes) for what a `Digest` carries.

### Subscriptions

Clients call `resources/subscribe` with a URI. When the underlying cluster state changes, the server sends
`notifications/resources/updated` for that URI and the client re-reads. `notifications/resources/list_changed` fires
when resources are created or removed. Both are ACL-filtered per notification: a client is only notified about
resources its identity can read.

### Argument completion

Templated resource URIs and prompt arguments complete. A client editing `cetacean://services/{id}`, or a prompt's
`service` argument, can call `completion/complete` and receive the **names** of the matching resources — not IDs,
since a name is what a human picks from a list and what a read now resolves.

Matching is a case-insensitive substring, the same rule `find`'s `query` uses: Docker names carry their stack as a
prefix, so a caller typing `prometheus` is offered `monitoring_prometheus`. Completions are capped at 100 values,
with `total` and `hasMore` reporting what was left out.

Completion reads the same ACL-filtered listing every other read goes through, so it never reveals a resource the
caller could not otherwise enumerate, and a secret's payload is redacted on that path before a name is taken from it.

## Compact resource shapes

MCP tools and resource reads never hand back a raw Docker Engine object — eight services as raw `swarm.Service`
run to roughly fourteen thousand tokens, most of it `Platforms` entries and a duplicated `PreviousSpec`, and the
field a caller actually asked for (is this thing healthy?) isn't in there at all, since health is derived from
tasks rather than read off the spec. Instead, every list and every detail read returns one of two compact,
transport-neutral shapes.

**Row** — one entry in a list, returned by `find`. Every row carries `id`, `name` and `type` (singular: `service`,
`node`, `task`, ...). `stack` (owning namespace), `state` (derived condition) and `detail` (the single most
identifying secondary fact — a service's image, a node's role, a network's driver) are omitted where they don't
apply. `desired`/`running` are populated only where a replica count means something, and the three cases genuinely
differ:

- **services** set both — the desired replica count and how many are currently running;
- **stacks** set `desired` alone, and it counts *member services*, not replicas;
- everything else — **nodes, tasks, configs, secrets, networks, volumes** — sets neither.

**Digest** — the detail view of one resource, returned by `describe` and by every templated
`cetacean://<type>/{id}` resource read (`services/{id}/logs` is the exception — a log stream, not a digest).
Alongside `id`/`name`/`type`/`state`, it adds:

- `reason` — the cause Swarm gave for a non-healthy `state`; omitted when the state is healthy.
- `since` — when the current state began: the oldest still-live failing task's timestamp, or the resource's own
  last-updated time when there's no failure to date it from.
- `details` — a type-specific map of facts (a service's image, replica counts, reserved CPU/memory, ports,
  placement constraints; a node's role and capacity; a network's driver and subnets; ...). Deliberately untyped in
  the advertised schema — pinning it down would mean eight `describe_<type>` tools instead of one `describe`.
- `related` — always an array, never omitted, even when empty: the resources this one references or is
  referenced by, so a caller can traverse without a second search.
- `recentFailures` — always an array, never omitted: the task failures behind a failing state, newest first,
  capped at 5. Only a service digest ever populates it; every other type reports an empty array.
- `restarts` — a service's involuntary task terminations, as `lastHour` and `lastWeek` counts. Services only;
  omitted for every other type. This is the only field that reveals a restart loop: `state` reads `running`
  whenever a replica happens to be up, and `recentFailures` deliberately drops the tasks the orchestrator has
  already replaced, so a service crash-looping every few seconds otherwise describes as healthy. The two windows
  answer different questions — `lastHour` whether it is failing *now*, the ratio between them whether the fault
  is new or long-standing.

Every numeric field in `details` names its unit — `cpuLimitCores` (a float, in cores) and `memoryLimitBytes` (in
bytes) on a service digest, for instance — or is reported as a duration string like `"10s"` instead
(`healthcheckInterval`, an update policy's `delay`/`monitor`). Docker's own types express these as unlabelled
NanoCPU or nanosecond integers, which read as arbitrary large numbers. Environment variables are reported as
`envNames`: names only, never values.

### The `raw: true` escape hatch

Both `find` (when `type` is given) and `describe` accept `raw: true`, adding the untouched Docker record to the
result under `raw`. It exists so nothing the compact representation drops becomes permanently unreachable — but it
is the deliberately expensive escape hatch: the whole point of the compact shapes is to avoid handing an agent the
several-hundred-line object, so reach for `raw` only when a specific field a row or digest genuinely omits is
needed.

The raw records ride *beside* the compact shape rather than replacing it — `find` fills a `raw` array holding one
record per row it returned, in the same order, and `describe` adds a `raw` object next to the digest's fields. A
tool that advertises an output schema must return structured content conforming to it, and a tool has one schema
whatever its arguments, so raw has to be an addition to the declared shape rather than a substitute for it. The
compact half of a raw result is the same one a plain call returns, filters and paging included.

## Icons

Every tool and resource advertises an `icon` that MCP clients can render beside it. Tool icons are grouped by verb
category (read, search, scale, edit, node, remove); resource icons reflect the resource type (node, service, stack,
config, secret, network, volume, task, service logs, cluster, recommendations, history).

The server itself carries the same identity a tool does: a display `title`, a `description`, a `websiteUrl` and an
icon, so a host listing several MCP servers shows Cetacean by name rather than by its programmatic id.

The icons are plain SVGs served by Cetacean itself under the unauthenticated `/assets/mcp-icons/` prefix, so a client
loads them without a bearer token in every auth mode. Their URLs are absolute and derived from the canonical external
base URL — so when Cetacean runs behind a reverse proxy, **set `CETACEAN_MCP_ISSUER`** (the same value the OAuth
issuer and token audience use) or the icon URLs will point at the wrong host. If no external base URL can be resolved,
icons are omitted rather than advertised as broken relative links.

## Tools

Tools are gated by operations level (`CETACEAN_MCP_OPERATIONS_LEVEL`, defaulting to `CETACEAN_OPERATIONS_LEVEL`) and
by per-resource ACL write permission. A tool above the configured tier is not registered at all; a tool the identity
lacks grants for is hidden from `tools/list` and refused at call time. Each tool advertises the behavioural hints
(`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`) that clients use to gate confirmation prompts.
On connect, the server also sends top-level usage `instructions` (read-mostly model, resolve IDs via `find` first,
writes gated by tier + ACL) so agents know how to drive it. Each tool also advertises an `icon` grouped by verb
category (read, search, scale, edit, node, remove) that clients can render (see [Icons](#icons)).

Tool results carry machine-readable `structuredContent` (the parsed JSON object) alongside the text form. `find`
and `describe` additionally attach `resource_link` content items for the resources their result is about — the rows
of a listing, and for `describe` the resource itself plus everything it cross-references — so a host can offer
somewhere to go next and a client can `resources/read` one without the model first working out how to spell a
`cetacean://` URI. The links describe the page actually returned, filters and paging included, and are capped at 25:
they are an affordance, not the payload, and every id is in `structuredContent` regardless. Every registered tool
advertises an output schema that the server validates results against, on every call — a tool declaring a schema
must answer with conforming structured content whatever its arguments, so no argument switches a tool to a shape
its schema does not describe. An input-validation failure comes back as a tool result with `isError: true` (so the
model can self-correct), not a protocol error.

**Tier 0 — reads** (always available): `get_logs`, `find`, `describe`, `get_topology`, `get_metrics`,
`get_recommendations`.

`find` locates cluster resources. Give `type` (plural: `nodes`, `services`, `tasks`, `stacks`, `configs`,
`secrets`, `networks`, or `volumes`) to enumerate that type, paged, as a list of `Row`s — optionally narrowed by
`query` (name substring), `state`, `stack`, `node` (tasks only), `image` (services only) or `label` (`key` or
`key=value`); `limit`/`offset` page the result (default and max `limit` 200). Omit `type` and give `query` to
search by name, label or image reference across every type at once instead — one flat list sorted by name, each
row carrying its own `type`, the way the two tools it replaces (`list_resources` and `search`) did between them;
`limit` there instead bounds matches per resource type (default 3), and `offset` is ignored. Because there is no
paging in that mode, a cross-type result also carries `counts` — the per-type breakdown behind `total`, the same
field the HTTP search response uses — so "137 matches, showing 6" is legible without a second call, and narrowing
with `type` is the way to page through one of them. A typed listing omits `counts`: there `total` already means
one type, and `offset` reaches the rest. `raw: true` adds each match's untouched Docker record under `raw`,
beside the rows — see [Compact resource shapes](#compact-resource-shapes).

`describe` returns everything needed to act on one resource, as a `Digest`: its derived state, the reason behind
an unhealthy one, how long it has held, type-specific details, cross-references, and the recent task failures
behind a failing state. Its `type` argument is **singular** (`service`, `node`, `task`, `stack`, `config`,
`secret`, `network`, `volume`) — the reverse of `find`'s plural, and an easy thing to get backwards. Both `type`
and `id` are required; `id` accepts an ID or a name — a hostname for a node, the rendered `<service>.<slot>`
for a task, the plain name for everything else. A name that matches more than one resource (Swarm does not enforce
unique node hostnames) is refused, naming the matching IDs, rather than resolving to one of them. `raw: true`
adds the untouched Docker record under `raw`, beside the digest. Secret payloads and environment variable values
are never returned, in either mode.

`get_logs` reads either a service or a single task, named by `service` or `task` — exactly one, since the two are
different streams and guessing between them would return output the caller did not ask for. A service merges the
output of its live replicas; a task reads one replica, and is the only way to reach a replica that has already
exited, because a dead replica's lines are no longer in the service stream. That makes the task form the one to
reach for after a crash. It is also the more perishable: Swarm keeps a task's output only while it keeps the task
record, five per replica slot by default (`--task-history-limit`), so a service restarting in a loop retains only
seconds of history and should be read before anything else. A task's read grant is its parent service's, the same
key `remove_task` writes against.

`get_metrics` charts CPU, memory or network use for one service or one node over the last hour, six hours, day or
week. It takes a target and a metric rather than PromQL: Cetacean owns the queries, resolves the service or node
against its own cache, and checks the caller's read grant before querying — a tool accepting a raw query would hand
the caller a label selector of their own and with it a way around every grant. It needs Prometheus
(`CETACEAN_PROMETHEUS_URL`), plus cAdvisor for service metrics and node-exporter for node metrics; without them it
reports that metrics are unavailable rather than returning empty series.

`get_recommendations` returns the same findings as `cetacean://recommendations`, optionally filtered to one severity,
as a tool a host can render — see [Widgets](#widgets-mcp-apps). Its totals count what the caller may read, not what
the engine holds. A finding that has a remedy carries it as `fix` — `{"tool": "update_node", "section":
"availability"}` — naming the tool to call. The REST API states the same remedy as a route (`fixAction`), which an
MCP caller cannot issue; a finding whose route has no tool behind it reports no `fix` at all rather than a path
that cannot be followed.

**Tier 1 — operational**: `scale_service`, `update_service_image`, `rollback_service`, `restart_service`,
`remove_task`.

**Tier 2 — configuration**: `update_service`, `update_node_labels`, `create_secret`, `create_config`,
`update_service_secrets`, `update_service_configs`, `update_service_mounts`.

**Tier 3 — impactful / destructive**: `update_node`, `remove_service`, `remove_config`, `remove_secret`,
`remove_network`, `remove_volume`.

`update_service` names the part of the spec it changes in a `section` argument — `env`, `labels`, `resources`,
`placement`, `ports`, `update-policy`, `rollback-policy`, `log-driver`, `healthcheck` or `command` — and takes
that section's new value under `value`. It replaced eight tools that differed only in which field they wrote, and which between
them cost about two thousand tokens of every `tools/list`. The trade is that `value` is whatever the section
takes, so the input schema cannot describe it and the section's decoder validates it instead, naming the section
and the shape it wanted when a payload does not fit.

`update_node` works the same way over `availability` and `role`. Node **labels** stay a tool of their own,
`update_node_labels`, because they sit at a lower operations tier: folding them in would have meant a deployment
that lets an agent relabel a node also lets it demote a manager.

`healthcheck` takes the probe as `test` (Docker's exec form, e.g. `["CMD-SHELL", "curl -f localhost || exit 1"]`)
plus optional `interval`, `timeout`, `startPeriod` and `retries`. Durations are strings — `"10s"`, `"1m30s"` — never
nanosecond integers, which are a scale a caller sooner or later writes wrong. An empty `test` removes the check.
This is what closes the loop on `get_recommendations`, which reports services with no health check and until now
could not be acted on. `command` takes `command` (the entrypoint) and `args`, the split Docker itself makes.

Secrets, configs and mounts are **not** sections of `update_service`. Each takes a list rather than a spec fragment,
and attaching a secret checks a read grant on the secret as well as a write grant on the service, so each is a tool
of its own — `update_service_secrets`, `update_service_configs` and `update_service_mounts`. Passing one as a
`section` is refused with the name of the tool to use instead.

All three replace the set wholesale: pass every entry the service should end up with, because one left out is
detached. `update_service_mounts` covers named volumes, host bind mounts and tmpfs — note that a bind mount hands
the container the host's filesystem at that path, and binding `/var/run/docker.sock` gives it control of the whole
cluster. The tool will do it if asked; the operations level is not what stops it.

Swarm secrets and configs are immutable, so rotating one is three calls in order: `create_secret` for the
replacement, `update_service_secrets` to repoint each service that uses it — `describe` the secret first, its
`related` array names them — and `remove_secret` for the old one once nothing references it. Cetacean could
previously only do the last of the three, which made the whole sequence impossible.

Every MCP tool requires the same operations level as the REST route for the same operation, so there is no separate
MCP tier table to keep in step. That includes the three attachment editors, which sit at tier 2 rather than being
raised for MCP alone: the operations level is the operator's single dial and means one thing whichever transport
they reach for.

The `env` and `labels` sections, and `update_node_labels`, follow JSON Merge Patch semantics: a `null` value
deletes a key, and the patch is applied against a fresh inspect of the live spec to avoid clobbering concurrent
changes. Every other section is a wholesale replacement — a field you omit is cleared. Mutating tools return `409` on a
Docker version conflict.

## Prompts

Prompts are named sequences a client offers from a menu: picking one seeds the
conversation with an investigation or a runbook, so an agent does not have to
rediscover which order of calls answers a question.

| Prompt | Tier | Argument | Reads | What it does |
|---|---|---|---|---|
| `diagnose_service` | 0 | `service` | `service` | Walks tasks, the failing task's logs, metrics and recent changes to find why a service is unhealthy |
| `explain_unschedulable` | 0 | `service` | `service`, `node` | Separates the causes of an unplaced task: placement constraints, node labels and platform, node availability and state, replica caps, reservations |
| `review_capacity` | 0 | — | `node` | Joins node capacity, reservations, real usage and sizing findings to say where the cluster is constrained |
| `roll_back_service` | 1 | `service` | `service` | Confirms a service is actually degraded, then rolls it back and waits for the replicas to run |
| `right_size_service` | 2 | `service` | `service` | Checks a sizing recommendation against measured use, then corrects the reservations |
| `drain_node` | 3 | `node` | `node`, `service` | Checks quorum and that the work can be placed elsewhere, then drains and confirms the tasks moved |

A prompt's tier is the highest tier of the tools it walks, so it is never
offered where one of its steps would be refused. `CETACEAN_MCP_OPERATIONS_LEVEL`
therefore controls prompts as it controls tools: at the default tier 1 you get
the three diagnostic prompts plus `roll_back_service`.

Prompts are also filtered by ACL, all-or-nothing: a prompt is offered only when
**every** tool it walks is available to you *and* you hold read on every
resource type in its Reads column. A sequence whose fourth step you cannot
perform would dead-end partway, and for a remediation prompt possibly after a
write. The read-type check is the second half because `find`, `get_metrics`
and `get_recommendations` are deliberately ungated — each ACL-filters its own
results, so each stays visible to every caller — and a sequence built only
from those would otherwise be offered to someone who would get an empty list
from every step. A caller whose grants match nothing is offered no prompts at
all. A prompt you cannot see reports `not found` from `prompts/get`, the same
as a name that does not exist.

Prompts read no cluster data. They expand to a single message with the resource
name you supplied interpolated, and the name is not checked for existence — the
text tells the model to resolve it with `find` first. A prompt is a plan for
the model to carry out, not a report; every read and write it describes still
goes through the ordinary tool and resource paths, with the ordinary ACL checks.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `CETACEAN_MCP` | `false` | Enable the MCP server |
| `CETACEAN_MCP_OPERATIONS_LEVEL` | inherits `CETACEAN_OPERATIONS_LEVEL` | Tier ceiling for MCP tools (0–3) |
| `CETACEAN_MCP_ISSUER` | derived from listen addr + TLS | Canonical external base URL for the OAuth issuer and MCP audience; set this behind a reverse proxy |
| `CETACEAN_MCP_SIGNING_KEY` | auto-generated | HMAC-SHA256 JWT signing key |
| `CETACEAN_MCP_ACCESS_TOKEN_TTL` | `1h` | Access token lifetime |
| `CETACEAN_MCP_REFRESH_TOKEN_TTL` | `720h` | Refresh token lifetime (30 days) |
| `CETACEAN_MCP_CONSENT_TTL` | `2160h` | How long a remembered approval lasts (90 days); `0` disables remembering |
| `CETACEAN_MCP_MAX_CONCURRENT_TASKS` | `32` | Cap on in-flight task-augmented tool calls |
| `CETACEAN_MCP_TASK_TTL` | `15m` | Task retention applied when a call omits `task.ttl`; `0` disables the fill-in |
| `CETACEAN_MCP_MAX_TASK_TTL` | `1h` | Ceiling on the retention a call may ask for; `0` disables the cap |
| `CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR` | `true` | Require the RFC 8707 `resource` parameter |
| `CETACEAN_MCP_DCR_ENABLED` | `true` | Enable Dynamic Client Registration |
| `CETACEAN_MCP_DCR_RATE_LIMIT` | `10` | DCR registrations per IP per hour |
| `CETACEAN_MCP_DCR_MAX_CLIENTS` | `1000` | Global cap on registered clients (LRU-evicted) |
| `CETACEAN_MCP_CIMD_ENABLED` | `true` | Enable Client ID Metadata Documents |
| `CETACEAN_MCP_AUTH_BYPASS` | — | Auth modes that skip OAuth (e.g. `cert`) |

All settings are also available under the `[mcp]` and `[mcp.oauth]` TOML tables. When Cetacean runs behind a reverse
proxy, **always set `CETACEAN_MCP_ISSUER`** to the externally reachable base URL — token audiences and discovery URLs
are derived from it, and a wrong value breaks the OAuth flow.

## Tasks: mutations that finish when the cluster does

Docker's write APIs return the moment Swarm *accepts* a spec change. Scaling a service to five replicas succeeds
instantly and tells you nothing about whether five replicas are running — the image may still be pulling, a
placement constraint may be unsatisfiable, the rollout may be halfway through. An agent that treats the call
returning as the change being done will act on a cluster that is not there yet.

The `2026-07-28` Tasks extension fixes that. Four tools accept task augmentation:

| Tool | Converged when |
|---|---|
| `scale_service` | running replicas match the desired count, no rolling update in flight |
| `update_service_image` | as above, after the rollout finishes |
| `rollback_service` | as above |
| `restart_service` | as above |

Send `params.task` on the `tools/call` and the server answers immediately with a task handle instead of the tool's
result. **Always include a `ttl`** — see [Task retention](#task-retention-and-taskttl) below:

```json
{"method":"tools/call","params":{"name":"scale_service","arguments":{"id":"web","replicas":5},"task":{"ttl":600000}}}
```

Poll `tasks/get` with the returned `taskId`. The task stays `working` until Cetacean's cache shows the cluster has
actually converged, then flips to `completed`; a mutation Docker refuses — or one the ACL denies — ends `failed`
with the reason in `statusMessage`. `tasks/cancel` is supported; `tasks/list` was removed by this revision.

These four return a summary of where the service ended up rather than its full specification — the result is
retained for the task's lifetime, and a summary is the more useful answer after a scale or a rollback anyway:

```json
{"id":"web","name":"web","image":"nginx:1.27","mode":"replicated","replicas":5,"running":5,"state":"running","version":42}
```

`running` is the live count, `state` is the same derivation the dashboard and REST API report, and `version` is the
Swarm version index for a caller doing its own concurrency checks. `replicas` is omitted for a global service,
which has no desired count. The shape is advertised as an `outputSchema`, so a client can rely on it.

The spec-editing tools answer in the same spirit, reporting the section they changed rather than the whole
service:

```json
{"id":"web","name":"web","version":42,"section":"resources",
 "details":{"cpuLimitCores":2,"memoryLimitBytes":1073741824}}
```

`details` is the same projection `describe` builds for that resource, narrowed to the edited section, so
confirming an edit and describing the resource afterwards cannot disagree. The node tools
(`update_node_labels`, `update_node`) answer with `id`, `hostname`, `version`,
`section` and `details`, plus `role` and `availability` on every one of them — draining a manager is a different
act from draining a worker, and "did the drain take" is not answerable without the role it took effect on.

This is also what keeps a credential out of the answer. These tools used to return the whole `swarm.Service`, so
a call that only raised a CPU limit came back carrying the service's environment variable values and its log
driver's options — a `splunk-token` among them. The projection reports `envNames` and the log driver's
`optionNames`, never the values, which is the rule the rest of the interface already followed.

Task augmentation is **optional** on all four. A plain `tools/call` with no `params.task` behaves exactly as
before, returning as soon as Docker accepts the change.

Two limits are worth knowing. A task gives up after five minutes and fails, on the reasoning that a mutation which
has not converged by then will not converge on its own. And `tasks/cancel` marks the task cancelled for the client
but does not stop the convergence watcher, which runs to convergence or timeout regardless — it only polls an
in-memory cache, so the cost is negligible. `CETACEAN_MCP_MAX_CONCURRENT_TASKS` (default 32) caps how many run at
once.

### Task retention and `task.ttl`

`ttl` is milliseconds from task **creation**, after which the server discards the task and its result. Send one on
every task-augmented call:

```json
"task": {"ttl": 600000}
```

Ten minutes comfortably covers the five-minute convergence bound while leaving time to read the result. Pick a
`ttl` long enough that you will have polled `tasks/get` before it elapses — once the task is discarded, the result
is gone.

The protocol says an omitted `ttl` means *no expiration*, which would retain the result for the lifetime of the
server process. Cetacean does not honour that literally, because nothing else bounds it:
`CETACEAN_MCP_MAX_CONCURRENT_TASKS` caps how many tasks run *concurrently*, not how many completed ones are kept —
the counter is released when a task finishes, but its record is not. An agent mutating services on a schedule would
grow the server's memory use steadily, for as long as it runs.

Two settings bound it instead:

| Setting | Default | Effect |
|---|---|---|
| `CETACEAN_MCP_TASK_TTL` | `15m` | Applied when a call omits `ttl`, or sends `0` or `null` |
| `CETACEAN_MCP_MAX_TASK_TTL` | `1h` | Ceiling on what a call may ask for |

A request above the ceiling is **served with the ceiling**, not refused — you asked for a cluster mutation, and
failing it over a retention preference would be the wrong trade. It appears in the debug log, not in the response.
Set either to `0` to disable that half: no fill-in, or no ceiling.

The default leaves at least ten minutes to collect a result even for a task that ran the full convergence timeout,
since the clock starts at creation rather than completion. Keep that margin in mind if you change it; setting
`CETACEAN_MCP_TASK_TTL=0` puts a client that omits `ttl` back to retaining its result until the process exits.

The four task-capable tools return a compact summary rather than the full service specification, which bounds the
per-task cost as well as the count.

## Widgets (MCP Apps)

A host that supports the MCP Apps extension can render Cetacean's data as an interactive view instead of JSON.
Cetacean advertises `io.modelcontextprotocol/ui` and serves each widget as a resource:

```
ui://cetacean/table
ui://cetacean/topology
ui://cetacean/logs
ui://cetacean/metrics
ui://cetacean/recommendations
```

`ui://cetacean/table` renders a `find` result: a searchable, sortable table of one resource type, showing how many
records it holds when the page is a subset.

`ui://cetacean/topology` renders a `get_topology` result as a graph to pan, zoom and drag — services joined to the
overlay networks they attach to, or cluster nodes joined to the services they run. Switching between the two views
re-runs the tool, so the second view is fetched under the same identity and the same grants as the first.

`ui://cetacean/logs` renders a `get_logs` result as a live tail. It keeps calling `get_logs` from the cursor the
previous read returned — a widget cannot hold an SSE stream open, having no route to Cetacean's HTTP API — and
filtering by level or search term happens over the lines already fetched, without going back through the host.

`ui://cetacean/metrics` renders a `get_metrics` result as a line chart with a range picker; changing the range
re-runs the tool. Every series is named in a legend and carries its latest value as text, so the chart never leans
on colour alone to say which line is which.

`ui://cetacean/recommendations` renders a `get_recommendations` result as findings grouped by severity, most serious
first. Picking one asks the host to send the model a follow-up about that finding, which a host may decline.

Each tool names its widget in `_meta`, so a host knows which view fits the result; the same tool called from a
client without app support simply returns JSON.

Each is a single self-contained HTML document with MIME type `text/html;profile=mcp-app` — all CSS and JavaScript
inlined, because an app resource has no base URL and cannot fetch anything relative to itself.

Widgets read data by calling Cetacean's own MCP tools through the host, never by reaching Cetacean's HTTP API
directly. That keeps every read on the one audited path, so a widget sees exactly what the calling identity's ACL
grants allow, and it works even when the browser has no network route to the Cetacean host.

Each widget declares an empty `_meta.ui.csp`, which states that it needs no external origin at all — no network, no
third-party assets, no nested frames. This is deliberate rather than an omission: an absent policy and an empty one
mean different things to a host, and a widget that ever needs an origin should be a visible change.

Widgets are optional in both directions. A host without app support ignores the extension and receives ordinary
results, and a Cetacean binary built without `npm run build:widgets` serves no widget resources and does not
advertise the extension — rather than pointing a host at a view it cannot load.

## Distributed tracing

Point `CETACEAN_OTEL_ENDPOINT` (or `[tracing].endpoint`) at an OpenTelemetry collector that accepts OTLP over HTTP:

```bash
CETACEAN_OTEL_ENDPOINT=http://collector:4318
```

Cetacean then records a span for every MCP method it dispatches (`mcp.tools/call`, `mcp.resources/read`, …) and a
nested span for every tool handler (`tool.scale_service`), tagged with the method, the tool name, the negotiated
protocol version, and an error status when the call fails.

The point of it is joining traces rather than collecting isolated ones. A caller that is already tracing can put W3C
trace context in the request — either in the `_meta` property bag as `traceparent` / `tracestate` / `baggage`, the
transport-agnostic convention MCP `2026-07-28` specifies, or in the usual HTTP headers — and Cetacean's spans become
children of the caller's span. So an agent's "scale the web service" turn and the Docker call it produced appear in
one trace.

Tracing is off unless the endpoint is set, and nothing is allocated for it in that case. A malformed endpoint stops
startup with an error: the OTLP exporter would otherwise accept it, fall back to `localhost:4318`, and export
nowhere while looking configured.

## Security notes

- Run MCP behind TLS in production. Cetacean logs a warning at startup if MCP is enabled without TLS and auth mode is
  not `none`.
- `CETACEAN_CORS_ORIGINS` applies to the OAuth browser redirects; set it when the consent flow crosses origins. It is
  also the allowlist for the `/mcp` endpoint's `Origin` check: a request carrying an `Origin` that isn't listed (and
  isn't `*`) is rejected with `403` — a DNS-rebinding defense required by the Streamable HTTP transport. Non-browser
  clients send no `Origin` and are unaffected.
- CIMD fetches are SSRF-guarded: `https`-only, private/reserved/CGNAT IP ranges blocked, DNS pinned to the validated
  address through connect, 5 KB / 5 s limits.

## Known limitations (current release)

- **Refresh tokens and consent approvals survive a restart; nothing else does.** They are written to
  `mcp-tokens.json` in the data directory (see [`storage.data_dir`](configuration.md)) on every issue, rotation,
  revocation, and approval, so an authorized client stays authorized and does not have to be re-approved. DCR
  registrations and authorization codes are still in-memory: a client whose registration is lost re-registers via
  the discovery chain on the next `401`, and a code lost mid-flow is indistinguishable from an expired one at its
  60-second TTL. Access-token JWTs are stateless and remain valid until they expire, unless
  `CETACEAN_MCP_SIGNING_KEY` is unset — an auto-generated key changes on every restart, which invalidates them. Set it
  to keep access tokens valid too. See [Remembered approvals](#remembered-approvals) for what makes a stored approval
  stop applying.
- **Durability is not shared.** The token file is local. Multi-replica deployments are not supported: authorization codes
  live in one replica's memory for their 60-second lifetime, and an unset `CETACEAN_MCP_SIGNING_KEY` leaves each
  replica signing with a different key.
- **Token revocation is not immediate.** Per RFC 7009 the revoke endpoint always returns `200`, but a revoked access
  token JWT keeps validating until `exp` (default 1h). Lower `CETACEAN_MCP_ACCESS_TOKEN_TTL` if you need a tighter
  window.
- **No cross-replica event replay.** A client that reconnects to a different replica catches up by re-reading
  resources rather than replaying missed notifications.
- **A task's result is discarded when its retention elapses.** Cetacean bounds retention itself
  (`CETACEAN_MCP_TASK_TTL`, `CETACEAN_MCP_MAX_TASK_TTL`) rather than honouring the protocol's "no expiration", which
  cannot be bounded. A client that polls `tasks/get` too late finds the task gone. See
  [Task retention](#task-retention-and-taskttl).
