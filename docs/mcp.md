---
title: MCP Server
description: Run Cetacean's embedded Model Context Protocol server, with OAuth 2.1 authorization, ACL enforcement, and long-running task support.
category: guide
tags: [mcp, ai, agents, oauth, automation]
---

# MCP Server

Cetacean embeds a [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server so AI agents can
read the same live cluster state the dashboard shows and make the same ACL-gated changes. It is a second
transport over the existing authentication, authorization, and operations level model. For the catalog of
tools, resources, prompts, and widgets an agent sees, see [MCP tools and resources](mcp-tools).

## Quick start

The server is disabled by default. Enable it with `CETACEAN_MCP=true`:

```bash
CETACEAN_MCP=true \
CETACEAN_AUTH_MODE=oidc \
CETACEAN_MCP_ISSUER=https://cetacean.example.com \
  ./cetacean
```

The endpoint is served at `{base_path}/mcp`. Point an MCP-capable client at it:

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

On first connect the client is challenged for authorization, walks the OAuth discovery chain, and prompts
you to sign in and consent. No client secret or manual registration is required.

## Protocol version and compatibility

The server speaks MCP streamable HTTP at revision `2026-07-28`, and only that revision. Older revisions are
refused with an `unsupported protocol version` JSON-RPC error naming the version to use, so upgrade an older
client. The deprecated HTTP+SSE and stdio transports are not supported.

There are no sessions: `2026-07-28` removed the `initialize` handshake and `Mcp-Session-Id`. Every request
carries its own protocol version, client identity, and capabilities in `_meta`, so there is nothing to
reconnect to. A client receives server-initiated notifications by opening a `subscriptions/listen` stream,
which replaces both `resources/subscribe` and the standalone `GET` stream; notification types are opt-in.

## Authentication and authorization

### Auth mode `none`

The OAuth endpoints are not registered and `/mcp` is unauthenticated. Anyone who can reach the endpoint has
whatever access the operations level allows. Use this only on trusted networks.

### OAuth 2.1 (auth modes `oidc`, `tailscale`, `headers`)

When MCP is enabled and the auth mode supports a browser flow, Cetacean acts as an OAuth 2.1 authorization
server and as a protected resource for `/mcp`, implementing the MCP `2026-07-28` authorization profile.

| Endpoint | Purpose |
|---|---|
| `GET {base}/.well-known/oauth-protected-resource` | Protected Resource Metadata (RFC 9728); advertises the authorization server |
| `GET {base}/.well-known/oauth-authorization-server` | Authorization Server Metadata (RFC 8414) |
| `GET {base}/.well-known/openid-configuration` | The same metadata at the OpenID Connect Discovery 1.0 location |
| `GET {base}/oauth/authorize` | Authorization endpoint; renders the consent screen |
| `POST {base}/oauth/token` | Token endpoint |
| `POST {base}/oauth/revoke` | Token revocation (RFC 7009) |
| `POST {base}/oauth/register` | Dynamic Client Registration (RFC 7591) |

The flow:

1. The client hits `/mcp` without a valid token and receives `401` with a `WWW-Authenticate` header pointing
   at the Protected Resource Metadata document.
2. The client discovers the authorization server, then identifies itself by CIMD or DCR (below).
3. The configured auth provider authenticates you, you see a consent screen, and you approve.
4. The client exchanges the authorization code (PKCE-S256, single-use, 60s) for an access token and a refresh
   token.
5. Subsequent MCP requests carry `Authorization: Bearer <token>`.

Two client identification paths are supported. Prefer CIMD for anything new.

- **Client ID Metadata Documents (CIMD)**, preferred by `2026-07-28`. The `client_id` is an `https://` URL
  pointing at a published metadata document, which Cetacean fetches and verifies; the consent screen shows a
  "verified via published metadata" badge. Advertised as `client_id_metadata_document_supported` and disabled
  with `mcp.oauth.cimd_enabled`, which refuses every `https://` client ID and makes no outbound fetch.
- **Dynamic Client Registration (RFC 7591)**, deprecated in `2026-07-28` and kept for compatibility. `POST`
  to `/oauth/register`. Every DCR client is public and PKCE-only; symmetric (`client_secret`) auth methods
  are rejected. A DCR client names itself, so the consent screen shows a "self-reported identity" badge, and
  registrations are held in memory and lost on restart.

Access tokens are HS256 JWTs with an `aud` claim equal to the canonical `/mcp` URL, so a token minted for one
deployment cannot be replayed against another. Resource indicators (RFC 8707) are required by default.
Refresh tokens are opaque, rotate single-use, and carry an absolute grant-family lifetime; presenting a
rotated token revokes the whole family.

### Remembered approvals

Approving a client is remembered, so you are asked once rather than every time its refresh token expires. A
record ties your identity to that client, the MCP endpoint it asked for, and a fingerprint of the client's
name and redirect URIs as you were shown them. You are asked again when any of those change, including when
a client updates its own metadata document, and when the grant is revoked or Cetacean detects a stolen
refresh token. Clients that registered dynamically are never remembered, because their metadata is
self-reported.

An approval lasts `mcp.consent_ttl` (default 90 days) from the moment you approved, and approving again
renews it. Set it to `0s` to turn remembering off: nothing is recorded, existing records stop being honoured,
and every authorization reaches a human.

Approvals live in `mcp-tokens.json` beside the refresh tokens, at mode `0600`. Refresh tokens are stored as
SHA-256 hashes, but an approval is a capability: anyone able to write that file can pre-approve a client.

### `cert` mode and auth bypass

mTLS client-certificate auth cannot drive a browser consent flow, so OAuth is not used in `cert` mode. Set
`mcp.oauth.auth_bypass` to `cert` to let mTLS-authenticated clients reach `/mcp` with their client
certificate, deriving identity from the certificate and skipping the bearer token.

### ACL enforcement

Every resource read, tool call, and notification is checked against the ACL policy for the request's
identity. Nothing is cached per session, so policy hot reloads take effect immediately. `tools/list` is
filtered per identity, so an operator with read-only grants sees only the read tools. Resource reads return
identical `not found` errors whether a resource is absent or denied, so the policy does not leak existence.
See [Authorization](authorization) for the grant model.

Catalog filtering projects grants onto resource types, expanding them the way a call does: a `stack:X` grant
reaches the services, tasks, configs, secrets, networks, and volumes in that stack, a `service:X` grant
reaches that service's tasks, and `write` implies `read`. The projection over-approximates, so
`service:web-*` reports the service type whether or not a matching service exists, but every listed tool is
still checked against the named resource when invoked.

## Configuration

These four settings are what you need to get running:

| Setting | Env var | Default | Description |
|---|---|---|---|
| `mcp.enabled` | `CETACEAN_MCP` | `false` | Enable the MCP server |
| `mcp.issuer` | `CETACEAN_MCP_ISSUER` | derived from listen address and TLS | Canonical external base URL for the OAuth issuer, token audience, and icon URLs |
| `mcp.signing_key` | `CETACEAN_MCP_SIGNING_KEY` | auto-generated | HMAC-SHA256 JWT signing key; `CETACEAN_MCP_SIGNING_KEY_FILE` reads it from a file, so it can arrive as a Docker secret |
| `mcp.operations_level` | `CETACEAN_MCP_OPERATIONS_LEVEL` | inherits `operations_level` | Ceiling for MCP tools (0 read-only, 1 operational, 2 configuration, 3 impactful) |

Behind a reverse proxy, always set `mcp.issuer` to the externally reachable base URL. Token audiences,
discovery URLs, and icon URLs are derived from it, and a wrong value breaks the OAuth flow.

The remaining settings, including token and consent lifetimes, task retention, and the DCR and CIMD controls,
are documented in [Configuration](configuration).

## Tasks

Docker's write APIs return the moment Swarm accepts a spec change, which says nothing about whether the
change took effect: the image may still be pulling, a placement constraint may be unsatisfiable, or the
rollout may be halfway through. The `2026-07-28` Tasks extension lets a call wait for the cluster instead.
Four tools accept task augmentation:

| Tool | Converged when |
|---|---|
| `scale_service` | Running replicas match the desired count and no rolling update is in flight |
| `update_service_image` | As above, after the rollout finishes |
| `rollback_service` | As above |
| `restart_service` | As above |

Send `params.task` on the `tools/call` and the server answers immediately with a task handle instead of the
tool's result. Include a `ttl`:

```json
{"method":"tools/call","params":{"name":"scale_service","arguments":{"id":"web","replicas":5},"task":{"ttl":600000}}}
```

Poll `tasks/get` with the returned `taskId`. The task stays `working` until Cetacean's cache shows the cluster
has converged, then flips to `completed`. A mutation Docker refuses, or one the ACL denies, ends `failed`
with the reason in `statusMessage`. `tasks/cancel` is supported; this revision removed `tasks/list`.

Task augmentation is optional on all four: a plain `tools/call` with no `params.task` returns as soon as
Docker accepts the change.

A task gives up after five minutes and fails. `tasks/cancel` marks the task cancelled for the client without
stopping the convergence watcher, which polls an in-memory cache until convergence or timeout.
`mcp.max_concurrent_tasks` (default 32) caps how many run at once.

### Task retention

`ttl` is milliseconds from task creation, after which the server discards the task and its result. Pick one
long enough that you will have polled `tasks/get` before it elapses; ten minutes covers the five-minute
convergence bound with room to read the result.

The protocol says an omitted `ttl` means no expiration, which would retain the result for the lifetime of the
process. Cetacean does not honour that literally, since `mcp.max_concurrent_tasks` caps concurrency rather
than retention. Two settings bound retention instead:

| Setting | Default | Effect |
|---|---|---|
| `mcp.task_ttl` | `15m` | Applied when a call omits `ttl`, or sends `0` or `null` |
| `mcp.max_task_ttl` | `1h` | Ceiling on what a call may ask for |

A request above the ceiling is served with the ceiling rather than refused, and the clamp appears in the
debug log, not in the response. Set either setting to `0s` to disable that half. Because the clock starts at
creation, the `15m` default leaves at least ten minutes to collect a result even for a task that ran the full
convergence timeout.

## Distributed tracing

Point `CETACEAN_OTEL_ENDPOINT` (or `tracing.endpoint`) at an OpenTelemetry collector that accepts OTLP over
HTTP:

```bash
CETACEAN_OTEL_ENDPOINT=http://collector:4318
```

Cetacean records a span for every MCP method it dispatches (`mcp.tools/call`, `mcp.resources/read`, and so
on) and a nested span for every tool handler (`tool.scale_service`), tagged with the method, the tool name,
the negotiated protocol version, and an error status when the call fails.

A caller that is already tracing can put W3C trace context in the request, either in the `_meta` property bag
as `traceparent`, `tracestate`, and `baggage` (the transport-agnostic convention MCP `2026-07-28` specifies)
or in the usual HTTP headers. Cetacean's spans then become children of the caller's span, so an agent's turn
and the Docker call it produced appear in one trace.

Tracing is off unless the endpoint is set. A malformed endpoint stops startup with an error, since the OTLP
exporter would otherwise fall back to `localhost:4318` and export nowhere while looking configured.

## Security notes

- Run MCP behind TLS in production. Cetacean logs a warning at startup if MCP is enabled without TLS and the
  auth mode is not `none`.
- `CETACEAN_CORS_ORIGINS` applies to the OAuth browser redirects; set it when the consent flow crosses
  origins. It is also the allowlist for the `/mcp` endpoint's `Origin` check: a request carrying an `Origin`
  that is not listed (and is not `*`) is rejected with `403`, the DNS-rebinding defense the Streamable HTTP
  transport requires. Non-browser clients send no `Origin` and are unaffected.
- CIMD fetches are SSRF-guarded: `https` only, private, reserved, and CGNAT IP ranges blocked, DNS pinned to
  the validated address through connect, and 5 KB / 5 s limits.

## Known limitations

- Refresh tokens and consent approvals survive a restart; nothing else does. They are written to
  `mcp-tokens.json` in the data directory (see [`storage.data_dir`](configuration)) on every issue, rotation,
  revocation, and approval, so an authorized client stays authorized. DCR registrations and authorization
  codes are in memory: a client whose registration is lost re-registers via the discovery chain on the next
  `401`, and a code lost mid-flow is indistinguishable from one expired at its 60-second TTL. Access-token
  JWTs stay valid until they expire, unless `mcp.signing_key` is unset, since an auto-generated key changes on
  every restart and invalidates them. Set it to keep access tokens valid too.
- Multi-replica deployments are not supported. The token file is local, authorization codes live in one
  replica's memory for their 60-second lifetime, and an unset `mcp.signing_key` leaves each replica signing
  with a different key. A client that reconnects to a different replica catches up by re-reading resources
  rather than replaying missed notifications.
- Token revocation is not immediate. Per RFC 7009 the revoke endpoint always returns `200`, but a revoked
  access-token JWT keeps validating until `exp` (default 1h). Lower `mcp.access_token_ttl` for a tighter
  window.
- A task's result is discarded when its retention elapses, so a client that polls `tasks/get` too late finds
  the task gone. See [Task retention](#task-retention).
