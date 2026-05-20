# MCP Server Post-Implementation Review — Tracking

**Date:** 2026-05-20
**Source reviews:** parallel agents covering OAuth/security, MCP server core, and shared cluster + integration. Findings consolidated from `feat/mcp-server` branch.

Each issue carries an ID, severity, confidence, file:line citation, summary, and a proposed fix. Update the **Status** column as work lands.

| ID | Severity | Status | Title |
|----|----------|--------|-------|
| M-01 | Critical | fixed | MCP `resources/read` skips ACL entirely |
| M-02 | Critical | fixed | `toolSearch` skips ACL filtering |
| M-03 | Critical | fixed | Notifications dispatched without ACL recheck |
| M-04 | Important | open | Refresh-token store leaks `consumed` / `grants` unboundedly |
| M-05 | Important | open | Authorization codes never expire from store |
| M-06 | Important | open | `toolSearch` accepts empty query → cluster dump |
| M-07 | Important | open | `CETACEAN_MCP_AUTH_BYPASS` is dead config |
| M-08 | Important | open | Missing `destructiveHint` on tier-3 node mutations |
| M-09 | Important | open | PKCE verifier uses `==` instead of `hmac.Equal` |
| M-10 | Important | open | `WWW-Authenticate` header built with Go `%q` |
| M-11 | Important | open | `Server.Close()` not concurrency-safe despite comment |
| M-12 | Minor | open | DCR doesn't validate `grant_types` / `response_types` |
| M-13 | Minor | open | Integration test misses the riskiest paths |
| M-14 | Minor | open | CIMD fetcher errors leak to consent error page |

---

## Critical — ACL bypasses on the MCP transport

These three nullify the ACL policy whenever a non-trivial policy is configured. Land before any ACL-using operator enables MCP.

### M-01 — `resources/read` skips ACL entirely

**File:** `internal/mcp/resources.go:128–244` (`lookupResource`)
**Confidence:** 100

Every per-resource case (`nodes/{id}`, `services/{id}`, `tasks/{id}`, `stacks/{name}`, `configs/{id}`, `secrets/{id}`, `networks/{id}`, `volumes/{name}`) reads from the cache and returns the object without calling `acl.Evaluator.Can()`. The REST detail handlers consistently apply ACL filtering. The design doc requires "Checked on every operation, not cached per-session."

**Fix:** Add `checkRead(ctx, resourceType, resourceName)` mirroring `checkWrite` in `tools.go`. Call it from each per-resource case after the cache lookup resolves the ID to a name. Skip for the three static resources (`cluster`, `recommendations`, `history`) — those have no per-resource ACL semantic.

---

### M-02 — `toolSearch` skips ACL filtering

**File:** `internal/mcp/tools.go:492–501`
**Confidence:** 100

`toolSearch` calls `cluster.Search` and marshals raw results. The REST `HandleSearch` (`internal/api/search_handlers.go:84–95`) wraps the identical call in a per-type `acl.Filter` pass. A restricted identity can enumerate service / node / secret names across ACL boundaries via the `search` tool.

**Fix:** Mirror the REST handler — for each `SearchResults.Hits` slot, run `acl.Filter` against the identity from `auth.IdentityFromContext(ctx)`. The `searchACLPrefix` map and `aclResourceFor` helper in `api/search_handlers.go` can be moved to `internal/cluster/` (or duplicated; map is small).

---

### M-03 — Notifications dispatched without ACL recheck

**File:** `internal/mcp/notifications.go:198–216` (`dispatchCacheEvent`)
**Confidence:** 95

`notifications/resources/updated` is sent to every session subscribed to a URI; `notifications/resources/list_changed` is broadcast to every initialized client. Neither path consults ACL. The design spec is explicit: "does the identity have read access? (ACL re-checked per notification)."

**Fix:** `NotificationManager` does not currently store identity per session. Either:

1. Capture `*auth.Identity` at `OnAfterSubscribe` time and stash it on the subscription record, or
2. Retrieve identity from the mcp-go session store at dispatch time.

Option (1) is simpler but stale identities persist across token refresh. Option (2) follows the "not cached per-session" rule from the design but needs a hook into mcp-go's session manager. Decide before implementing.

---

## Important — bugs and spec deviations

### M-04 — Refresh-token store leaks memory unboundedly

**File:** `internal/mcp/oauth/store.go:216–237` (`Rotate`)
**Confidence:** 95

`consumed` and `grants` are append-only outside `revokeGrantLocked` (only triggered by theft or explicit revocation). A client refreshing every hour for 30 days leaves 720 dead hashes in `consumed` + 721 entries in the `grants` slice. Compounds when the grant is allowed to expire naturally (step 4 of `Rotate` deletes only the live entry, orphaning the family records).

**Fix:** Bound the grant family. Two options:

1. Drop hashes from `consumed` and `grants[grantID]` once their parent entry is past `expiresAt + grace` — requires walking the family on each rotation (cheap because it's already in the lock).
2. Cap `grants[grantID]` to a fixed window (e.g. last 32 rotations); older `consumed` entries get pruned alongside. Theft detection still works for the recent window; ancient replay attempts go undetected but those tokens already expired.

Add a periodic GC if neither path catches every leak.

---

### M-05 — Authorization codes never expire from store

**File:** `internal/mcp/oauth/store.go:48–99` (`AuthCodeStore`)
**Confidence:** 80

Codes are only evicted on `Redeem`. Abandoned flows (client never returns) leave 60-second-expired entries in `codes` until process restart. Low-grade DoS vector against the `/oauth/authorize` endpoint.

**Fix:** Background sweeper goroutine running every minute, or sweep inline on each `Issue` call.

---

### M-06 — `toolSearch` accepts empty query

**File:** `internal/mcp/tools.go:493–500`
**Confidence:** 85

`RequireString("query")` returns `""` without erroring; `cluster.Search("")` matches everything because `containsFoldNoAlloc` is true for an empty substring. An agent passing `{"query": ""}` receives up to 1000 entries per resource type. REST guards at `search_handlers.go:59–62`.

**Fix:** `if strings.TrimSpace(query) == "" { return nil, fmt.Errorf("query must not be empty") }` after `RequireString`.

---

### M-07 — `CETACEAN_MCP_AUTH_BYPASS` is dead config

**File:** `internal/config/mcp.go:60–61`, `internal/mcp/server.go` (no reader)
**Confidence:** 88

Loaded from env + TOML, documented in CLAUDE.md and the design doc, but `bearerAuth` (`server.go:172–194`) never consults it. Operators setting `CETACEAN_MCP_AUTH_BYPASS=cert` get 401s with no signal. The struct comment also says "client IDs" while CLAUDE.md / spec say "auth modes" — disagreement before implementation.

**Fix:** Resolve the semantic disagreement first. The spec intent is "skip OAuth for these upstream auth modes" — i.e. requests authenticated by mTLS cert middleware should be allowed to talk to `/mcp` without a Bearer token, deriving identity from the cert. Implement in `bearerAuth`: when the request's `auth.Identity` (set by upstream cert middleware) is present and the configured `AuthBypass` list contains the upstream mode, skip JWT verification and use the upstream identity directly. Update the struct comment.

---

### M-08 — Missing `destructiveHint` on tier-3 node mutations

**File:** `internal/mcp/tools.go:357–379`
**Confidence:** 85

`update_node_availability` (drain) and `update_node_role` (demote) carry no `WithDestructiveHintAnnotation(true)`. The design's "destructiveHint (true for tier 3 removals)" can be read narrowly, but draining a node or demoting the last manager is at least as impactful as any single removal. MCP clients use the hint to gate confirmation prompts — this is user-visible behavior.

**Fix:** Add `mcplib.WithDestructiveHintAnnotation(true)` to both tools.

---

### M-09 — PKCE verifier uses `==`

**File:** `internal/mcp/oauth/server.go:298–305` (`verifySHA256Challenge`)
**Confidence:** 88

String equality in Go is not constant-time. Practical exploitability is low (one shot per code), but `consent.go:194` correctly uses `hmac.Equal` for the CSRF token — inconsistency invites future bugs and security auditors will flag it.

**Fix:** `return hmac.Equal([]byte(computed), []byte(challenge))`.

---

### M-10 — `WWW-Authenticate` header built with Go `%q`

**File:** `internal/mcp/oauth/server.go:583–586`
**Confidence:** 90

Go's `%q` produces Go-syntax double-quoted strings, not RFC 7230 quoted-string. Happens to match for URL / identifier values today, but is wrong by spec and breaks for any value containing a backtick or non-ASCII rune.

**Fix:** Build the value with explicit `"` literals and HTTP quoted-string escape rules (backslash-escape only `"` and `\`). A small helper is worth extracting since this is the only place we emit `WWW-Authenticate`.

---

### M-11 — `Server.Close()` not concurrency-safe despite comment

**File:** `internal/mcp/server.go:149–154`
**Confidence:** 82

The comment says "Safe to call multiple times" but the body reads + writes `s.cancelNotifications` without synchronization. Concurrent calls race; a duplicate invocation of the underlying cache cancel may or may not be idempotent.

**Fix:** Wrap the body in `sync.Once`.

---

## Minor

### M-12 — DCR doesn't validate `grant_types` / `response_types`

**File:** `internal/mcp/oauth/dcr.go:193–210`
**Confidence:** 85

RFC 7591 §3.2 requires `invalid_client_metadata` for unsupported values. No security consequence today (token endpoint only routes `authorization_code` / `refresh_token`) but it's a spec deviation that an aggressive client validator may flag.

**Fix:** Reject anything that isn't a subset of `["authorization_code", "refresh_token"]` for `grant_types` and `["code"]` for `response_types`.

---

### M-13 — Integration test misses the riskiest paths

**File:** `internal/mcp/integration_test.go`
**Confidence:** 82

`TestMCPEndToEnd` runs with `OAuth = nil` and no ACL evaluator. Missing coverage:

- Bearer-auth 401 with `WWW-Authenticate` (covers M-10 once fixed)
- ACL read denial on `resources/read` (covers M-01)
- ACL filter on `search` tool (covers M-02)
- Subscribe → mutate cache → receive `notifications/resources/updated` (covers M-03)
- Refresh-token rotation through the HTTP handler
- CIMD path end-to-end (unit-tested in `cimd_test.go`, never wired)

**Fix:** Extend the integration test with each scenario, or add focused tests in `internal/mcp/` and `internal/mcp/oauth/`. The ACL scenarios pair naturally with the M-01 / M-02 / M-03 fixes.

---

### M-14 — CIMD fetcher errors leak to consent error page

**File:** `internal/mcp/oauth/server.go` (`resolveClientMeta` → `renderErrorPage`)
**Confidence:** 70

Raw fetcher errors (DNS, SSRF block reasons, "connection refused", etc.) reach the browser via the consent error page. Acceptable for an admin-facing tool; worth surfacing here so it can be revisited if MCP exposure expands.

**Fix:** Wrap external-fetch errors in a generic "client metadata could not be retrieved" message before rendering; keep the specific reason in the server log.

---

## Suggested ordering

1. **M-01, M-02, M-03** — these three together; they share the `auth.IdentityFromContext` plumbing and the integration test (M-13) lands alongside.
2. **M-06** — one-line guard, ship with the M-02 patch.
3. **M-04, M-05** — token store hygiene; one PR.
4. **M-07** — needs a design call on bypass semantics first.
5. **M-08, M-09, M-10, M-11, M-12, M-14** — bundle as a quality pass.
