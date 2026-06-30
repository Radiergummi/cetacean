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

The server speaks MCP **streamable HTTP** and negotiates the protocol revision per client, supporting `2025-11-25`
(latest) down to `2024-11-05`. Clients that negotiate `2025-06-18` or newer receive richer responses (structured
tool output, output schemas); older clients transparently fall back to the text representation. The deprecated
HTTP+SSE transport and stdio transport are not supported.

Sessions are stateful: the server issues an `Mcp-Session-Id` on `initialize` and uses it to route server-initiated
notifications (resource updates, list changes). Sessions are reconnect-friendly — a client that loses its session
re-initializes and re-subscribes; bearer tokens are valid on any replica that shares the signing key.

## Authentication and authorization

### When auth mode is `none`

The OAuth endpoints are not registered and `/mcp` is unauthenticated. Anyone who can reach the endpoint has whatever
access the operations level allows. Only appropriate for trusted networks.

### OAuth 2.1 (auth modes `oidc`, `tailscale`, `headers`)

When MCP is enabled and the auth mode supports a browser flow, Cetacean acts as an OAuth 2.1 authorization server
**and** identifies itself as a protected resource for `/mcp`. It implements the MCP `2025-11-25` authorization
profile:

| Endpoint | Purpose |
|---|---|
| `GET {base}/.well-known/oauth-protected-resource` | Protected Resource Metadata (RFC 9728) — advertises the AS |
| `GET {base}/.well-known/oauth-authorization-server` | Authorization Server Metadata (RFC 8414) |
| `GET {base}/oauth/authorize` | Authorization endpoint (renders the consent screen) |
| `POST {base}/oauth/token` | Token endpoint |
| `POST {base}/oauth/revoke` | Token revocation (RFC 7009) |
| `POST {base}/oauth/register` | Dynamic Client Registration (RFC 7591) |

The flow:

1. The client hits `/mcp` without a valid token and receives `401` with a `WWW-Authenticate` header pointing at the
   Protected Resource Metadata document.
2. The client discovers the authorization server, then identifies itself by one of:
   - **Dynamic Client Registration** — POST to `/oauth/register`. Every DCR client is public and PKCE-only;
     symmetric (`client_secret`) auth methods are rejected. Self-reported client names render with a "self-reported
     identity" badge on the consent screen.
   - **Client ID Metadata Documents (CIMD)** — the `client_id` is an `https://` URL pointing at a published metadata
     document. Cetacean fetches and verifies it (with SSRF protections), and renders a "verified via published
     metadata" badge. CIMD is the registration mechanism recommended by the `2025-11-25` spec.
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

## Tools

Tools are gated by operations level (`CETACEAN_MCP_OPERATIONS_LEVEL`, defaulting to `CETACEAN_OPERATIONS_LEVEL`) and
by per-resource ACL write permission. A tool above the configured tier is not registered at all; a tool the identity
lacks grants for is hidden from `tools/list` and refused at call time. Each tool advertises the behavioural hints
(`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`) that clients use to gate confirmation prompts.

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
| `CETACEAN_MCP_SESSION_IDLE_TTL` | `30m` | Idle session cleanup |
| `CETACEAN_MCP_MAX_SESSIONS` | `256` | Concurrent session limit |
| `CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR` | `true` | Require the RFC 8707 `resource` parameter |
| `CETACEAN_MCP_DCR_ENABLED` | `true` | Enable Dynamic Client Registration |
| `CETACEAN_MCP_DCR_RATE_LIMIT` | `10` | DCR registrations per IP per hour |
| `CETACEAN_MCP_DCR_MAX_CLIENTS` | `1000` | Global cap on registered clients (LRU-evicted) |
| `CETACEAN_MCP_CIMD_ENABLED` | `true` | Enable Client ID Metadata Documents |
| `CETACEAN_MCP_AUTH_BYPASS` | — | Auth modes that skip OAuth (e.g. `cert`) |

All settings are also available under the `[mcp]` and `[mcp.oauth]` TOML tables. When Cetacean runs behind a reverse
proxy, **always set `CETACEAN_MCP_ISSUER`** to the externally reachable base URL — token audiences and discovery URLs
are derived from it, and a wrong value breaks the OAuth flow.

## Security notes

- Run MCP behind TLS in production. Cetacean logs a warning at startup if MCP is enabled without TLS and auth mode is
  not `none`.
- `CETACEAN_CORS_ORIGINS` applies to the OAuth browser redirects; set it when the consent flow crosses origins.
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
