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

## Resources

Resources are read-only views. Detail resources mirror the REST API responses exactly (same enrichment, same secret
redaction). All return `application/json`.

**Static** (`resources/list`):

| URI | Description |
|---|---|
| `cetacean://cluster` | Swarm status, managers, raft, CA config |
| `cetacean://recommendations` | Current recommendation engine findings |
| `cetacean://history` | Recent resource change events |

**Templated** (`resources/templates/list`):

| URI template | Description |
|---|---|
| `cetacean://nodes/{id}` | Node detail |
| `cetacean://services/{id}` | Service detail with cross-references |
| `cetacean://services/{id}/logs` | Service logs, merged across replicas (subscribable) |
| `cetacean://tasks/{id}` | Task detail enriched with service name and node hostname |
| `cetacean://stacks/{name}` | Stack rollup with member resources |
| `cetacean://configs/{id}` | Config metadata + base64 data |
| `cetacean://secrets/{id}` | Secret metadata (data redacted) |
| `cetacean://networks/{id}` | Network detail |
| `cetacean://volumes/{name}` | Volume detail |

### Subscriptions

Clients call `resources/subscribe` with a URI. When the underlying cluster state changes, the server sends
`notifications/resources/updated` for that URI and the client re-reads. `notifications/resources/list_changed` fires
when resources are created or removed. Both are ACL-filtered per notification: a client is only notified about
resources its identity can read.

## Icons

Every tool and resource advertises an `icon` that MCP clients can render beside it. Tool icons are grouped by verb
category (read, search, scale, edit, node, remove); resource icons reflect the resource type (node, service, stack,
config, secret, network, volume, task, service logs, cluster, recommendations, history).

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
On connect, the server also sends top-level usage `instructions` (read-mostly model, resolve IDs via `search` first,
writes gated by tier + ACL) so agents know how to drive it. Each tool also advertises an `icon` grouped by verb
category (read, search, scale, edit, node, remove) that clients can render (see [Icons](#icons)).

Tool results carry machine-readable `structuredContent` (the parsed JSON object) alongside the text form. The
`search`, `get_logs`, and `remove_*` tools additionally advertise an output schema that the server validates results
against. An input-validation failure comes back as a tool result with `isError: true` (so the model can self-correct),
not a protocol error.

**Tier 0 — reads** (always available): `get_logs`, `search`.

**Tier 1 — operational**: `scale_service`, `update_service_image`, `rollback_service`, `restart_service`,
`remove_task`.

**Tier 2 — configuration**: `update_service_env`, `update_service_labels`, `update_node_labels`,
`update_service_resources`, `update_service_placement`, `update_service_ports`, `update_service_update_policy`,
`update_service_rollback_policy`, `update_service_log_driver`.

**Tier 3 — impactful / destructive**: `update_node_availability`, `update_node_role`, `remove_service`,
`remove_config`, `remove_secret`, `remove_network`, `remove_volume`.

Env-var and label tools follow JSON Merge Patch semantics: a `null` value deletes a key, and the patch is applied
against a fresh inspect of the live spec to avoid clobbering concurrent changes. Mutating tools return `409` on a
Docker version conflict.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `CETACEAN_MCP` | `false` | Enable the MCP server |
| `CETACEAN_MCP_OPERATIONS_LEVEL` | inherits `CETACEAN_OPERATIONS_LEVEL` | Tier ceiling for MCP tools (0–3) |
| `CETACEAN_MCP_ISSUER` | derived from listen addr + TLS | Canonical external base URL for the OAuth issuer and MCP audience; set this behind a reverse proxy |
| `CETACEAN_MCP_SIGNING_KEY` | auto-generated | HMAC-SHA256 JWT signing key |
| `CETACEAN_MCP_ACCESS_TOKEN_TTL` | `1h` | Access token lifetime |
| `CETACEAN_MCP_REFRESH_TOKEN_TTL` | `720h` | Refresh token lifetime (30 days) |
| `CETACEAN_MCP_MAX_CONCURRENT_TASKS` | `32` | Cap on in-flight task-augmented tool calls |
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
result:

```json
{"method":"tools/call","params":{"name":"scale_service","arguments":{"id":"web","replicas":5},"task":{}}}
```

Poll `tasks/get` with the returned `taskId`. The task stays `working` until Cetacean's cache shows the cluster has
actually converged, then flips to `completed`; a mutation Docker refuses — or one the ACL denies — ends `failed`
with the reason in `statusMessage`. `tasks/cancel` is supported; `tasks/list` was removed by this revision.

Task augmentation is **optional** on all four. A plain `tools/call` with no `params.task` behaves exactly as
before, returning as soon as Docker accepts the change.

Two limits are worth knowing. A task gives up after five minutes and fails, on the reasoning that a mutation which
has not converged by then will not converge on its own. And `tasks/cancel` marks the task cancelled for the client
but does not stop the convergence watcher, which runs to convergence or timeout regardless — it only polls an
in-memory cache, so the cost is negligible. `CETACEAN_MCP_MAX_CONCURRENT_TASKS` (default 32) caps how many run at
once.

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

- **State is in-memory.** DCR registrations, authorization codes, and refresh tokens do not survive a restart;
  clients silently re-authorize via the discovery chain on the next `401`. Access-token JWTs remain valid until they
  expire. This is acceptable for single-instance and small multi-replica deployments.
- **Token revocation is not immediate.** Per RFC 7009 the revoke endpoint always returns `200`, but a revoked access
  token JWT keeps validating until `exp` (default 1h). Lower `CETACEAN_MCP_ACCESS_TOKEN_TTL` if you need a tighter
  window.
- **No cross-replica event replay.** A client that reconnects to a different replica catches up by re-reading
  resources rather than replaying missed notifications.
