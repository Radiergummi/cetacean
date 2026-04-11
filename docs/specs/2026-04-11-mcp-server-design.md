# MCP Server Design

**Date:** 2026-04-11

## Summary

Embed an MCP (Model Context Protocol) server in Cetacean, exposing cluster state as resources and write operations as tools to AI agents. Uses streamable HTTP transport with stateful sessions, OAuth 2.1 for authorization (with CIMD for client identification), and integrates with the existing auth, ACL, and operations level systems. Enabled via `CETACEAN_MCP=true`.

## Motivation

Cetacean holds a complete, real-time view of a Docker Swarm cluster. An MCP server makes this view available to AI agents (Claude Code, etc.), turning Cetacean from a dashboard humans look at into a cluster interface agents can reason over. Primary use case: DevOps/SRE using an AI assistant for incident triage and routine cluster management, with a path toward autonomous agents later.

## Transport

Streamable HTTP (MCP spec 2025-03-26). Single endpoint at `{base_path}/mcp`. All JSON-RPC requests and responses go through this path. Server can upgrade responses to SSE for streaming notifications.

Stateful sessions: the server issues `Mcp-Session-Id` headers. Sessions enable server-initiated notifications (resource changes, log streams) without polling.

No support for the deprecated SSE transport or stdio.

## Integration Architecture

The MCP server is mounted as an `http.Handler` on Cetacean's existing `http.ServeMux`. It shares the same port, TLS config, and base path.

```
Client (Claude Code, etc.)
  |
  POST {base_path}/mcp  (JSON-RPC over streamable HTTP)
  |
  +-- Middleware: requestID -> recovery -> securityHeaders -> cors -> auth -> requestLogger
  |
  +-- StreamableHTTPServer.ServeHTTP()
  |     +-- Session management (Mcp-Session-Id)
  |     +-- JSON-RPC dispatch
  |     |    +-- resources/list, resources/read, resources/subscribe
  |     |    +-- tools/list, tools/call
  |     |    +-- initialize, ping
  |     +-- SSE streaming (server -> client notifications)
  |
  +-- Handlers read from: Cache, Recommendations Engine
  +-- Handlers write via: DockerWriteClient interfaces
  +-- Auth identity: injected via WithHTTPContextFunc from auth middleware
  +-- ACL checks: Evaluator.Can() / acl.Filter() per request
```

The `negotiate` middleware is skipped for `/mcp` (always JSON-RPC). Auth middleware runs before the MCP handler. `WithHTTPContextFunc` bridges the authenticated `Identity` from Cetacean's HTTP context into mcp-go's context.

### Library

[mcp-go](https://github.com/mark3labs/mcp-go) (`mark3labs/mcp-go`). Provides `StreamableHTTPServer` implementing `http.Handler`, stateful session management with custom `SessionIdManager`, `WithHTTPContextFunc` for context injection, and per-session resources/tools.

## OAuth 2.1 Authorization Server

When `CETACEAN_MCP=true` and auth mode is not `none`, Cetacean exposes an OAuth 2.1 authorization server for MCP client authorization.

### Endpoints

```
GET  {base_path}/.well-known/oauth-authorization-server   -> server metadata (RFC 8414)
GET  {base_path}/oauth/authorize                          -> authorization endpoint
POST {base_path}/oauth/token                              -> token endpoint
POST {base_path}/oauth/revoke                             -> token revocation (RFC 7009)
```

### Flow

1. MCP client discovers OAuth metadata from `/.well-known/oauth-authorization-server`.
2. Client redirects user to `/oauth/authorize?response_type=code&client_id=...&code_challenge=...`.
3. Cetacean's auth middleware authenticates the user using whatever auth mode is configured (OIDC redirects to IdP, Tailscale reads identity from connection, cert reads client certificate, headers reads proxy headers).
4. User sees a server-rendered HTML consent screen showing the verified client name/logo (from CIMD) and the redirect URI. This is not part of the SPA -- it's a standalone page served by the OAuth handler, since the flow is initiated by external MCP clients.
5. Cetacean issues an authorization code and redirects back to the client.
6. Client exchanges the code for access token + refresh token at `/oauth/token`.
7. Subsequent MCP requests include `Authorization: Bearer <token>`.

This works with all auth providers because the authorization endpoint sits behind the existing auth middleware. By the time the consent screen renders, Cetacean knows who the user is regardless of auth mode.

When auth mode is `none`, the OAuth endpoints are not registered and the `/mcp` endpoint is unauthenticated.

### Client Identification (CIMD)

Client identification uses OAuth Client ID Metadata Documents ([draft-ietf-oauth-client-id-metadata-document-01](https://www.ietf.org/archive/id/draft-ietf-oauth-client-id-metadata-document-01.html)). The `client_id` is an `https://` URL pointing to a JSON metadata document.

When Cetacean receives an authorization request:

1. Fetch the document at the `client_id` URL (with SSRF protections, see Security section).
2. Validate the `client_id` field in the document matches the URL (exact string comparison).
3. Validate `redirect_uris` includes the requested `redirect_uri`.
4. Reject symmetric auth methods (`client_secret_post`, `client_secret_basic`).
5. Display `client_name` and `logo_uri` on the consent screen.
6. Cache the metadata in-memory (1-hour TTL).

If the `client_id` is not a URL (plain string), Cetacean treats it as an unverified public client. The consent screen shows the raw ID with a warning.

### Tokens

- **Access tokens**: JWTs signed with HMAC-SHA256 using the shared signing key. Claims: `sub` (subject), `groups`, `iss` (this Cetacean instance), `aud` ("mcp"), `exp`, `iat`, `jti`. Default 1-hour expiry.
- **Refresh tokens**: opaque, cryptographically random, stored in-memory as a hash. Default 30-day expiry. Rotation on each use (new refresh token issued, old one invalidated). If a revoked refresh token is presented, the entire grant is revoked (token theft detection).
- **Authorization codes**: cryptographically random, single-use, 60-second expiry. Bound to `client_id`, `redirect_uri`, `code_challenge`, and authenticated identity.

JWTs with a shared signing key are designed for multi-replica deployments. Any Cetacean replica can validate any token without cross-replica communication. Refresh tokens are in-memory and per-replica; a token refresh that hits a different replica requires re-authorization. This is an acceptable trade-off for the initial version.

## Resources

### Resource Templates

Advertised via `resources/templates/list`:

| URI Template | Description | MIME Type |
|---|---|---|
| `cetacean://nodes/{id}` | Node detail | `application/json` |
| `cetacean://services/{id}` | Service detail | `application/json` |
| `cetacean://services/{id}/logs` | Service logs (subscribable) | `application/json` |
| `cetacean://tasks/{id}` | Enriched task detail | `application/json` |
| `cetacean://stacks/{name}` | Stack detail with members | `application/json` |
| `cetacean://configs/{id}` | Config metadata + base64 data | `application/json` |
| `cetacean://secrets/{id}` | Secret metadata (data redacted) | `application/json` |
| `cetacean://networks/{id}` | Network detail | `application/json` |
| `cetacean://volumes/{name}` | Volume detail | `application/json` |

### Static Resources

Advertised via `resources/list`:

| URI | Description |
|---|---|
| `cetacean://cluster` | Cluster info (swarm status, managers, raft, CA config) |
| `cetacean://recommendations` | Current recommendation findings |
| `cetacean://history` | Recent change history (last 100 entries) |

### Content Format

All resources return `application/json` text content. JSON structure mirrors the existing REST API responses: same cache data, same enrichment (e.g., `ServiceName` on tasks), same secret redaction.

### Subscriptions

- Client calls `resources/subscribe` with a URI (e.g., `cetacean://services/abc123`).
- Server registers the subscription on the session.
- When the cache fires an `OnChange` event matching that resource, server sends `notifications/resources/updated` with the URI (per MCP spec, no payload -- client re-reads).
- `notifications/resources/list_changed` fires when resources are created or removed.
- Log subscriptions (`cetacean://services/{id}/logs`): notification fires when new log lines are available. Client re-reads with an opaque cursor returned in the previous read to get only new lines.
- Subscriptions are ACL-filtered per-notification: the client only gets notifications for resources they can read.

## Tools

### Parameterized Reads (no tier gating)

| Tool | Parameters | Description |
|---|---|---|
| `get_logs` | `service`, `tail?`, `since?`, `level?` | One-shot log retrieval |
| `search` | `query`, `types?`, `limit?` | Global cross-resource search |

### Tier 1 -- Operational

| Tool | Parameters | Description |
|---|---|---|
| `scale_service` | `id`, `replicas` | Scale a service |
| `update_service_image` | `id`, `image` | Update a service's image |
| `rollback_service` | `id` | Rollback to previous spec |
| `restart_service` | `id` | Force restart |
| `remove_task` | `id` | Force-reschedule a task |

### Tier 2 -- Configuration

| Tool | Parameters | Description |
|---|---|---|
| `update_service_env` | `id`, `env` | Set env vars (merge patch) |
| `update_service_labels` | `id`, `labels` | Set service labels (merge patch) |
| `update_node_labels` | `id`, `labels` | Set node labels (merge patch) |
| `update_service_resources` | `id`, `resources` | Set CPU/memory limits/reservations |
| `update_service_placement` | `id`, `placement` | Set placement constraints |
| `update_service_ports` | `id`, `ports` | Set published ports |
| `update_service_update_policy` | `id`, `policy` | Set update config |
| `update_service_rollback_policy` | `id`, `policy` | Set rollback config |
| `update_service_log_driver` | `id`, `driver` | Set log driver |

### Tier 3 -- Impactful

| Tool | Parameters | Description |
|---|---|---|
| `update_node_availability` | `id`, `availability` | Drain/pause/active |
| `update_node_role` | `id`, `role` | Promote/demote |
| `remove_service` | `id` | Delete a service |
| `remove_config` | `id` | Delete a config |
| `remove_secret` | `id` | Delete a secret |
| `remove_network` | `id` | Delete a network |
| `remove_volume` | `name`, `force?` | Delete a volume |

### Tool Behavior

- Every tool call checks `requireLevel` (MCP operations tier) AND `acl.Can(identity, "write", resource)` before executing.
- If the tier is too low: structured error "this operation requires operations level N".
- If ACL denies: structured error "write access denied for resource type:name".
- Tool annotations include `readOnlyHint` (true for reads, false for writes) and `destructiveHint` (true for tier 3 removals).
- Tool list is filtered per-request: tools the identity can never use (due to tier or ACL) are omitted from `tools/list`.

## Session Lifecycle

### Creation

1. Client sends `POST /mcp` with `initialize` request.
2. `StreamableHTTPServer` creates a session, issues `Mcp-Session-Id`.
3. `WithHTTPContextFunc` extracts `auth.Identity` from HTTP context into MCP context.
4. Session stores the identity. All subsequent requests use it for ACL.
5. Server responds with capabilities: `resources` (subscribe + listChanged), `tools`.

### Session State (in-memory)

```
Session {
    ID            string
    Identity      *auth.Identity
    Subscriptions map[string]struct{}   // subscribed resource URIs
    LogCursors    map[string]string     // per-service log read cursor
}
```

### Notification Flow

```
cache.OnChange event
  -> MCP session manager
  -> for each active session:
      -> does the event match any subscription?
      -> does the identity have read access? (ACL re-checked per notification)
      -> if yes: send notifications/resources/updated
```

For list changes (resource created/removed), send `notifications/resources/list_changed` to all sessions with appropriate read access.

### Session Cleanup

- Idle TTL (configurable, default 30 minutes): sessions with no requests are cleaned up.
- Explicit termination via `DELETE /mcp` with `Mcp-Session-Id`.
- On cleanup: subscriptions removed, log cursors dropped, notification channel closed.

### Reconnection

Sessions are ephemeral and reconnect-friendly. Designed for multi-replica deployments without sticky sessions:

- Client reconnects with a new session on whatever replica it hits.
- JWT is valid on any replica (shared signing key).
- Client re-negotiates capabilities and re-subscribes to resources.
- Missed events: client re-reads resources to catch up. Cache history supports replay via `Last-Event-ID` but not across replicas in the initial version.

### Connection Limits

- Independent cap from SSE (default 256 MCP sessions).
- Configurable via `CETACEAN_MCP_MAX_SESSIONS`.
- Over limit: JSON-RPC error with `Retry-After` hint.

## Security

### OAuth 2.1 Authorization Server

**PKCE (required):**
- All authorization requests MUST include `code_challenge` and `code_challenge_method`.
- `S256` is mandatory to implement. `plain` MAY be accepted but `S256` MUST be preferred.
- `code_verifier` validated at token exchange; reject if missing or mismatched.

**Authorization codes:**
- Cryptographically random, single-use, 60-second expiry.
- Bound to `client_id`, `redirect_uri`, `code_challenge`, and authenticated identity.
- Consumed on first use; reject replays.

**Redirect URI validation:**
- Exact string comparison against `redirect_uris` in the CIMD document.
- No pattern matching, no wildcards.
- If mismatched: reject and show an error page. Do NOT redirect.
- HTTPS required except for loopback addresses in development.

**CSRF protection:**
- `state` parameter combined with PKCE provides CSRF protection.
- Consent form additionally uses a server-side CSRF token bound to the user's auth session.

**Consent screen:**
- Not iframeable: `Content-Security-Policy: frame-ancestors 'none'` + `X-Frame-Options: DENY`.
- Displays verified client name and logo from CIMD document.
- Shows the `redirect_uri` where tokens will be sent.
- Requires explicit user approval.

**Token responses:**
- Include `Cache-Control: no-store` and `Pragma: no-cache` headers.

**Token validation (on every MCP request):**
- Verify JWT signature.
- Verify `exp` (not expired), `iss` (this Cetacean instance), `aud` ("mcp").
- Extract identity (`sub`, `groups`) for ACL evaluation.
- Reject tokens not issued by this server (no token passthrough).

**Refresh token theft detection:**
- Each refresh issues a new refresh token and invalidates the old one.
- If a revoked refresh token is presented, revoke the entire grant.

### CIMD Fetch (SSRF Prevention)

Fetching client metadata documents is a server-side request to an attacker-controllable URL.

- `client_id` URL MUST use `https://`, MUST contain a path, MUST NOT contain fragments or credentials.
- Resolve DNS and validate the resolved IP before connecting. Block private/reserved ranges: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `127.0.0.0/8`, `169.254.0.0/16`, `fc00::/7`, `fe80::/10`, `::1`.
- Do not follow redirects to private IP ranges (validate each hop).
- 5KB response size limit.
- 5-second fetch timeout.
- Validate `client_id` in the document matches the fetch URL (exact string comparison).
- Cache in-memory with 1-hour TTL.

### MCP Session Security

**Session IDs:**
- Generated using `crypto/rand`. UUID v4 (128 bits of entropy).
- Not sequential, not predictable.

**Session-identity binding:**
- Each session bound to the authenticated identity at creation.
- Every request MUST verify the bearer token; sessions are NOT a substitute for authentication.
- If the token is expired or revoked, reject regardless of valid session ID.
- Internal session key: `<subject>:<session_id>` to prevent cross-user hijacking.

### Transport Security

- TLS strongly recommended for production.
- Log a warning at startup if MCP is enabled without TLS and auth mode is not `none`.

### ACL Enforcement

- Checked on every operation, not cached per-session. Policy hot-reload is immediately effective.
- Resource subscriptions filtered per-notification; policy changes can revoke visibility of events.
- Tool list re-evaluated on each `tools/list` request.

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `CETACEAN_MCP` | `false` | Enable MCP server |
| `CETACEAN_MCP_OPERATIONS_LEVEL` | value of `CETACEAN_OPERATIONS_LEVEL` | Operations level for MCP tools (0-3). Defaults to the global operations level if unset. |
| `CETACEAN_MCP_SIGNING_KEY` | (auto-generated) | JWT signing key (HMAC-SHA256). If unset, auto-generated and persisted to `CETACEAN_DATA_DIR/mcp-signing-key`. |
| `CETACEAN_MCP_ACCESS_TOKEN_TTL` | `1h` | Access token lifetime |
| `CETACEAN_MCP_REFRESH_TOKEN_TTL` | `720h` | Refresh token lifetime (30 days) |
| `CETACEAN_MCP_SESSION_IDLE_TTL` | `30m` | Idle session cleanup |
| `CETACEAN_MCP_MAX_SESSIONS` | `256` | Concurrent session limit |

### TOML

```toml
[mcp]
enabled = true
operations_level = 1
signing_key = "..."
access_token_ttl = "1h"
refresh_token_ttl = "720h"
session_idle_ttl = "30m"
max_sessions = 256
```

### Interactions with Existing Config

- `CETACEAN_OPERATIONS_LEVEL` is the fallback for MCP tool gating when `CETACEAN_MCP_OPERATIONS_LEVEL` is unset.
- `CETACEAN_ACL_POLICY` / `CETACEAN_ACL_POLICY_FILE` apply to MCP reads and writes.
- `CETACEAN_BASE_PATH` prefixes `/mcp` and `/oauth/*` endpoints.
- `CETACEAN_CORS_ORIGINS` applies to MCP (OAuth flow involves browser redirects).

## Package Structure

### New Packages

```
internal/mcp/              -- MCP server setup, session manager, notification bridge
internal/mcp/resources.go  -- resource handlers (read from cache, format as JSON)
internal/mcp/tools.go      -- tool handlers (call DockerWriteClient, return results)
internal/mcp/oauth/        -- OAuth 2.1 AS: authorize, token, revoke, CIMD, JWT issuance
```

### Dependency Graph

```
internal/mcp/
  +-- reads from:  cache.Cache (all getters, history)
  +-- writes via:  docker.Client (DockerWriteClient interfaces)
  +-- authz via:   acl.Evaluator (Can, Filter)
  +-- identity:    auth.Identity (from HTTP context)
  +-- config:      config (operations level, MCP settings)
  +-- external:    github.com/mark3labs/mcp-go/server

internal/mcp/oauth/
  +-- identity:    auth.Identity (from HTTP context, post-auth-middleware)
  +-- signing:     crypto/hmac (JWT issuance/validation)
  +-- CIMD fetch:  net/http client (fetch client metadata documents)
  +-- no dependency on mcp-go (pure OAuth, independent of MCP protocol)
```

### Integration Point

In `internal/api/router.go`:

```go
if cfg.MCP.Enabled {
    mcpServer := mcp.New(cache, dockerClient, aclEvaluator, cfg.MCP)
    mux.Handle("{base}/mcp", mcpHandler(mcpServer))
    if authProvider != nil {
        oauth.RegisterRoutes(mux, authProvider, cfg.MCP)
    }
}
```

The MCP package depends on existing interfaces. It does not import `docker/watcher`, `api/handlers`, or the SSE broadcaster. It is a new consumer of the same cache and write client, not a layer on top of the REST API.
