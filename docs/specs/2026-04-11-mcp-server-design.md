# MCP Server Design

**Date:** 2026-04-11

## Summary

Embed an MCP (Model Context Protocol) server in Cetacean, exposing cluster state as resources and write operations as tools to AI agents. Uses streamable HTTP transport with stateful sessions, OAuth 2.1 for authorization (with CIMD for client identification), and integrates with the existing auth, ACL, and operations level systems. Enabled via `CETACEAN_MCP=true`.

## Motivation

Cetacean holds a complete, real-time view of a Docker Swarm cluster. An MCP server makes this view available to AI agents (Claude Code, etc.), turning Cetacean from a dashboard humans look at into a cluster interface agents can reason over. Primary use case: DevOps/SRE using an AI assistant for incident triage and routine cluster management, with a path toward autonomous agents later.

## Protocol and library baseline

- **MCP protocol version:** target the latest streamable-HTTP revision supported by the pinned `mcp-go` release (the 2025-03-26 transport with the 2025-06-18 authorization profile at minimum). Negotiate downward if the client requests an older `protocolVersion` on `initialize`.
- **Library:** [`github.com/mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go) at a pinned tagged release. The library is moving quickly; the implementation plan begins with a Task that pins a specific version, audits its current API surface (`NewStreamableHTTPServer`, `WithStateful`, `WithSessionIdleTTL`, `WithHTTPContextFunc`, tool/resource registration signatures), and updates the rest of the plan if any names have drifted. Re-validate at every dependency bump.

## Transport

Streamable HTTP. Single endpoint at `{base_path}/mcp`. All JSON-RPC requests and responses go through this path. Server can upgrade responses to SSE for streaming notifications.

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

When `CETACEAN_MCP=true` and auth mode is not `none`, Cetacean exposes an OAuth 2.1 authorization server **and** identifies itself as a protected resource for the `/mcp` endpoint. The implementation targets the MCP authorization profile as revised in the 2025-06-18 spec update (Resource Indicators, Protected Resource Metadata, AS discovery via `WWW-Authenticate`).

### Endpoints

```
GET  {base_path}/.well-known/oauth-authorization-server   -> AS metadata (RFC 8414)
GET  {base_path}/.well-known/oauth-protected-resource     -> PRM (RFC 9728), advertises the AS
GET  {base_path}/oauth/authorize                          -> authorization endpoint
POST {base_path}/oauth/token                              -> token endpoint
POST {base_path}/oauth/revoke                             -> token revocation (RFC 7009)
POST {base_path}/oauth/register                           -> Dynamic Client Registration (RFC 7591)
```

### Resource Server discovery

When an unauthenticated or invalidly-authenticated request hits `/mcp`, Cetacean responds:

```
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="mcp",
  resource_metadata="https://cetacean.example.com/.well-known/oauth-protected-resource",
  error="invalid_token"
```

The PRM document advertises:

```json
{
  "resource": "https://cetacean.example.com/mcp",
  "authorization_servers": ["https://cetacean.example.com"],
  "bearer_methods_supported": ["header"],
  "resource_documentation": "https://cetacean.example.com/api"
}
```

Modern MCP clients use this chain to discover the AS without out-of-band config.

### Resource Indicators (RFC 8707)

Every authorization and token request MUST include a `resource` parameter naming the MCP endpoint (e.g. `resource=https://cetacean.example.com/mcp`). Cetacean:

- Rejects authorize/token requests missing the `resource` parameter (or with a `resource` that doesn't match the MCP endpoint exactly).
- Issues access tokens with `aud` set to the canonical MCP endpoint URL — **not** the literal string `"mcp"`. Tokens are scoped to this specific Cetacean instance and cannot be replayed against another deployment.
- For backwards compatibility with older mcp-go clients that don't send `resource`, the server MAY accept the request and stamp the default audience; this is controlled by `CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR` (default `true`).

### Flow

1. Client hits `/mcp` without credentials → gets 401 with `WWW-Authenticate: ... resource_metadata=...`.
2. Client fetches PRM → discovers the AS URL.
3. Client fetches AS metadata at `/.well-known/oauth-authorization-server` → gets `registration_endpoint`, `authorization_endpoint`, `token_endpoint`.
4. Client identification: either
   - **DCR**: POST to `/oauth/register` with metadata, receive a client_id (and optionally `client_secret`, but we reject symmetric methods so DCR clients are public + PKCE-only).
   - **CIMD**: use an `https://` URL as `client_id` pointing to a self-hosted metadata document. Cetacean fetches and verifies it on first use.
5. Client redirects user to `/oauth/authorize?response_type=code&client_id=...&code_challenge=...&resource=...&state=...`.
6. Cetacean's auth middleware authenticates the user using whatever auth mode is configured.
7. User sees a server-rendered HTML consent screen showing the verified client name/logo and the redirect URI.
8. Cetacean issues an authorization code (bound to `client_id`, `redirect_uri`, `code_challenge`, `resource`, identity) and redirects back to the client.
9. Client exchanges the code for access token + refresh token at `/oauth/token` (must echo back the same `resource`).
10. Subsequent MCP requests include `Authorization: Bearer <token>`.

This works with web-capable auth providers (OIDC, headers, tailscale) because the authorization endpoint sits behind the existing auth middleware. By the time the consent screen renders, Cetacean knows who the user is.

**`cert` auth mode caveat:** mTLS-based auth doesn't work in a browser consent flow because the user-agent driving consent isn't the same TLS endpoint that presented the client certificate. With `cert` mode the OAuth endpoints return 503 with a `Cetacean-Auth-Hint` header telling the client that programmatic auth (cert) is the only path; in that case MCP clients should authenticate directly against `/mcp` with their own client cert and skip OAuth entirely. This is opt-in via `CETACEAN_MCP_AUTH_BYPASS=cert`.

When auth mode is `none`, the OAuth endpoints are not registered and the `/mcp` endpoint is unauthenticated.

### Client Identification

Two client identification paths are supported in parallel. Both produce identical access-token semantics; they differ only in how Cetacean learns about the client.

**Path A — Dynamic Client Registration (RFC 7591).** The default for ecosystem MCP clients (Claude Code, Cursor, etc.) that don't self-host metadata.

```
POST {base_path}/oauth/register
Content-Type: application/json

{
  "client_name": "Claude Code",
  "redirect_uris": ["http://localhost:33418/callback"],
  "token_endpoint_auth_method": "none",
  "grant_types": ["authorization_code", "refresh_token"],
  "response_types": ["code"]
}
```

Cetacean responds with a generated `client_id` (`cetacean-<random>`) and persists the registration in-memory (initial version) keyed by client_id. Symmetric auth methods (`client_secret_*`) are rejected — every DCR client is a public client and MUST use PKCE. Registrations are subject to a per-host rate limit (default 10/hour/IP) and a global cap (default 1000) to prevent abuse; over-cap registrations get 429.

DCR client metadata is **not** verified — anyone can register any `client_name`. The consent screen makes this visible: DCR-registered clients are flagged "Self-reported identity" with a yellow badge.

**Path B — Client ID Metadata Documents (CIMD).** For clients that publish their own metadata at a stable URL ([draft-ietf-oauth-client-id-metadata-document-01](https://datatracker.ietf.org/doc/draft-ietf-oauth-client-id-metadata-document/)). The `client_id` is an `https://` URL pointing to a JSON metadata document. CIMD is still an IETF draft, so the implementation is gated behind `CETACEAN_MCP_CIMD_ENABLED` (default `true`) so we can disable it if the draft changes incompatibly.

When Cetacean receives an authorization request with a URL-shaped `client_id`:

1. Fetch the document at the `client_id` URL (with SSRF protections, see Security section).
2. Validate the `client_id` field in the document matches the URL (exact string comparison).
3. Validate `redirect_uris` includes the requested `redirect_uri`.
4. Reject symmetric auth methods (`client_secret_post`, `client_secret_basic`).
5. Display `client_name` and `logo_uri` on the consent screen with a green "Verified via published metadata" badge.
6. Cache the metadata in-memory (1-hour TTL).

If the `client_id` is neither a URL nor a known DCR registration, the request is rejected with `invalid_client`.

### Tokens

- **Access tokens**: JWTs signed with HMAC-SHA256 (HS256) using the shared signing key. Claims: `sub` (subject), `groups`, `iss` (this Cetacean instance), `aud` (canonical MCP endpoint URL from the `resource` parameter), `exp`, `iat`, `jti`, `client_id`. Default 1-hour expiry. HS256 is chosen because the AS and RS are the same process — there's no third party validating tokens, so the symmetric/asymmetric distinction doesn't matter. If we later split the AS into a separate service this should migrate to RS256/ES256.
- **Refresh tokens**: opaque, cryptographically random, stored in-memory as a hash. Default 30-day expiry. Rotation on each use (new refresh token issued, old one invalidated). If a previously-rotated refresh token is presented again, the entire grant family is revoked (token theft detection).
- **Authorization codes**: cryptographically random, single-use, 60-second expiry. Bound to `client_id`, `redirect_uri`, `code_challenge`, `resource`, and authenticated identity.
- **Scopes are not used.** The access token grants whatever the underlying identity (sub + groups) is authorized to do via Cetacean ACLs. There is no MCP-specific scope vocabulary; access is enforced at every tool call and resource read through the existing `acl.Evaluator`. The token endpoint silently ignores any `scope` parameter.

**Persistence caveats (initial version):**
- DCR registrations, authorization codes, and refresh tokens are all in-memory. A restart logs every MCP client out (they will silently re-authorize via the discovery chain on next 401). Access-token JWTs survive restarts because they're self-contained — but only until they expire (default 1h). This is acceptable for v1; documented in user docs.
- JWTs with a shared signing key support multi-replica deployments at the access-token layer. Refresh tokens and DCR registrations are per-replica; a token refresh or registered-client lookup that hits a different replica requires re-auth.

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

### Per-Request Tool Filtering

The MCP spec lets clients call `tools/list`; we want the response to reflect *what this identity can do right now*, not the global registry. The global tier filter (drop tools above `effectiveOperationsLevel`) is done once at server construction and is cheap. ACL-based filtering is per-identity and must be evaluated on every `tools/list` call.

`mcp-go`'s `AddTool` registers tools on a shared registry, so we cannot rely on it for identity-scoped filtering. Two viable strategies:

1. **Intercept `tools/list` at the JSON-RPC layer** — register a custom handler that calls into mcp-go for the registry, then filters the response by walking each tool's "would this identity be allowed?" predicate (tier already filtered globally; ACL evaluated against the identity in context). This is the chosen approach: one extra hook, no upstream patches.
2. ~~Per-session MCPServer instances~~ — discarded: prohibitive memory and re-registration cost.

If mcp-go's current release doesn't expose a clean hook for intercepting `tools/list`, the Task 10 plan falls back to filtering inside each tool's *call* handler (returning `not_authorized` errors at call time) and accepts that `tools/list` over-reports until upstream gains the hook. This is captured as a TODO with a tracking link.

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
cache change event
  -> cache listener fan-out (broadcaster + MCP notification manager)
  -> MCP session manager:
      -> for each active session:
          -> does the event match any subscription?
          -> does the identity have read access? (ACL re-checked per notification)
          -> if yes: send notifications/resources/updated
```

For list changes (resource created/removed), send `notifications/resources/list_changed` to all sessions with appropriate read access.

**Cache listener prerequisite:** `cache.Cache` currently exposes only a single `SetOnChange` slot, owned by the SSE broadcaster. This is replaced with a multi-listener registry (`AddOnChangeListener(fn) (cancel func())`) so MCP notifications can subscribe without displacing the broadcaster. The migration is the first task in the implementation plan and ships independently of MCP.

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
- Verify `exp` (not expired), `iss` (this Cetacean instance), `aud` (must match the canonical MCP endpoint URL — prevents replay against a sibling Cetacean deployment).
- Extract identity (`sub`, `groups`) for ACL evaluation.
- Reject tokens not issued by this server (no token passthrough).
- On any validation failure, emit `401` with a `WWW-Authenticate` header carrying `resource_metadata=` and an `error=` code (`invalid_token`, `insufficient_scope`, etc.) per RFC 6750.

**Refresh token theft detection:**
- Each refresh issues a new refresh token and invalidates the old one.
- Consumed token hashes are retained until the grant's original expiry, indexed by `grant_id`.
- If a previously-rotated refresh token is presented again, revoke the entire grant family (all tokens sharing that `grant_id`).

**Dynamic Client Registration abuse:**
- Per-IP rate limit on `/oauth/register` (default 10/hour, configurable via `CETACEAN_MCP_DCR_RATE_LIMIT`).
- Global cap on registered clients (default 1000, configurable via `CETACEAN_MCP_DCR_MAX_CLIENTS`).
- Registrations are evicted LRU when the cap is hit.
- DCR clients cannot self-elevate to "verified" status; they always render with the self-reported badge on consent.

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
| `CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR` | `true` | Require RFC 8707 `resource` parameter on authorize/token |
| `CETACEAN_MCP_DCR_ENABLED` | `true` | Enable Dynamic Client Registration endpoint |
| `CETACEAN_MCP_DCR_RATE_LIMIT` | `10` | Max DCR registrations per IP per hour |
| `CETACEAN_MCP_DCR_MAX_CLIENTS` | `1000` | Global cap on registered clients |
| `CETACEAN_MCP_CIMD_ENABLED` | `true` | Enable Client ID Metadata Documents |
| `CETACEAN_MCP_AUTH_BYPASS` | — | Comma-separated auth modes that skip OAuth (e.g. `cert`) |

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

[mcp.oauth]
require_resource_indicator = true
dcr_enabled = true
dcr_rate_limit = 10
dcr_max_clients = 1000
cimd_enabled = true
auth_bypass = []  # e.g. ["cert"]
```

### Interactions with Existing Config

- `CETACEAN_OPERATIONS_LEVEL` is the fallback for MCP tool gating when `CETACEAN_MCP_OPERATIONS_LEVEL` is unset.
- `CETACEAN_ACL_POLICY` / `CETACEAN_ACL_POLICY_FILE` apply to MCP reads and writes.
- `CETACEAN_BASE_PATH` prefixes `/mcp` and `/oauth/*` endpoints.
- `CETACEAN_CORS_ORIGINS` applies to MCP (OAuth flow involves browser redirects).

## Shared Domain Layer

Introducing a second transport (MCP alongside REST) creates a drift risk: enrichment logic (building `EnrichedTask`, redacting secret data, resolving cross-references, assembling `StackDetail`) currently lives inline in REST handlers. Duplicating it in MCP handlers would lead to divergence over time.

### Approach

Extract the enrichment and domain logic into a shared `internal/cluster` package. Both REST and MCP handlers become thin transport adapters that call into it.

```
REST handler:   parse HTTP request   -> cluster.ServiceDetail(cache, id) -> JSON-LD wrap    -> HTTP response
MCP handler:    parse JSON-RPC       -> cluster.ServiceDetail(cache, id) -> MCP resource    -> JSON-RPC response
```

### Package: `internal/cluster`

```
internal/cluster/
  resources.go   -- NodeDetail, ServiceDetail, TaskDetail, StackDetail, ConfigDetail,
                    SecretDetail, NetworkDetail, VolumeDetail, ClusterInfo
                    (read from cache, enrich, redact, resolve cross-refs)
  search.go      -- Search(cache, query, types, limit)
  list.go        -- ListNodes, ListServices, etc. (with filter/sort/pagination)
  enrich.go      -- EnrichTask (add ServiceName, NodeHostname),
                    RedactSecret, ResolveCrossRefs
```

Each function takes the cache (and optionally the ACL evaluator for filtering) and returns a plain Go struct. The struct is the single source of truth for what a "service detail" or "enriched task" looks like. Transport layers serialize it however they need to.

### What Moves

- `EnrichedTask` construction (currently inline in REST task handlers) -> `cluster.EnrichTask`
- Secret data redaction (currently inline in multiple REST handlers) -> `cluster.RedactSecret`
- Cross-reference resolution (`ServicesUsingConfig`, etc. assembly) -> `cluster.ConfigDetail`, etc.
- Stack detail assembly -> `cluster.StackDetail`
- Global search logic -> `cluster.Search`

### What Stays Transport-Specific

- JSON-LD wrapping (`@context`, `@id`, `@type`) -- REST only
- ETag computation and `304 Not Modified` -- REST only
- MCP resource URI and MIME type formatting -- MCP only
- HTTP content negotiation -- REST only
- JSON-RPC error codes -- MCP only
- Pagination Link headers -- REST only

### Write Operations

Write operations have less drift risk because both transports call the same `DockerWriteClient` methods directly. Domain validation (e.g., valid replica count, valid availability value) is extracted into `internal/cluster` where it's non-trivial, but most write operations are simple pass-throughs to the Docker API.

### Refactoring Scope

Existing REST handlers are refactored as part of the MCP work: extract inline enrichment into `internal/cluster`, update REST handlers to call it. This is upfront cost that eliminates the class of bugs where one transport returns different data than the other. The REST API behavior does not change -- only the internal call structure.

## Package Structure

### New Packages

```
internal/cluster/          -- shared domain logic (enrichment, search, validation)
internal/mcp/              -- MCP server setup, session manager, notification bridge
internal/mcp/resources.go  -- resource handlers (call cluster layer, format as MCP resources)
internal/mcp/tools.go      -- tool handlers (call DockerWriteClient, return results)
internal/mcp/oauth/        -- OAuth 2.1 AS: authorize, token, revoke, CIMD, JWT issuance
```

### Dependency Graph

```
internal/cluster/
  +-- reads from:  cache.Cache (all getters, history)
  +-- authz via:   acl.Evaluator (Can, Filter) -- for filtered list operations
  +-- no external dependencies

internal/api/ (existing REST handlers, refactored)
  +-- calls:       cluster (enrichment, search, validation)
  +-- reads from:  cache.Cache (for direct access where no enrichment needed)
  +-- writes via:  docker.Client (DockerWriteClient interfaces)
  +-- authz via:   acl.Evaluator

internal/mcp/
  +-- calls:       cluster (enrichment, search, validation)
  +-- reads from:  cache.Cache (for subscriptions and notifications)
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

The MCP package depends on existing interfaces and the shared cluster layer. It does not import `api/handlers` or the SSE broadcaster.
