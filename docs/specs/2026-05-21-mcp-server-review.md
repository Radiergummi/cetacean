# MCP Server Post-Implementation Review — Tracking (Round 2)

**Date:** 2026-05-21
**Branch:** `feat/mcp-server`
**Source reviews:** five parallel code-review agents covering OAuth core, OAuth client identification (CIMD/DCR/consent/PRM), MCP server core + notifications + logs, MCP tools + resources + ACL, and the shared `internal/cluster` layer. Findings deduplicated, verified against source, and false positives dropped (notably: the alleged `AuthCodeStore.Issue` for-loop shadowing — Go scopes the loop variable to the loop block, so it isn't a bug).

Round 1 lives in [`2026-05-20-mcp-server-review.md`](2026-05-20-mcp-server-review.md) (all items closed).

Each issue has an ID, severity, file:line citation, summary, and a proposed fix. Update the **Status** column as work lands. Statuses: `open`, `in progress`, `fixed`, `wontfix`, `deferred`.

| ID | Severity | Status | Title |
|----|----------|--------|-------|
| M-15 | Critical | fixed | `get_logs` MCP tool bypasses ACL on the target service |
| M-16 | Critical | fixed | `remove_config` / `remove_secret` / `remove_network` ACL keys use IDs instead of names |
| M-17 | Critical | fixed | CIMD SSRF guard has a DNS-rebinding TOCTOU window |
| M-18 | Critical | fixed | PKCE `code_verifier` length not enforced (RFC 7636 §4.1 violation) |
| M-19 | High     | fixed | Refresh-token grant lifetime resets on every rotation |
| M-20 | High     | fixed | Refresh-token resource mismatch revokes the family on a client typo |
| M-21 | High     | fixed | `tools/list` not filtered by per-identity ACL |
| M-22 | High     | fixed | `notifications/resources/list_changed` broadcast with no ACL check |
| M-23 | High     | fixed | Stack and volume detail ACL check runs before cache lookup → existence leak |
| M-24 | High     | fixed | `remove_task` ACL diverges from REST (keys `task:<id>` vs REST's `service:<name>`) |
| M-25 | Medium   | fixed | `discardingResponseWriter.Header()` returns a fresh map per call |
| M-26 | Medium   | fixed | `update_service_env` / `update_service_labels` reject `null` (breaks merge-patch delete) |
| M-27 | Medium   | fixed | DCR rate-limit keyed on `r.RemoteAddr` — bypassed behind a reverse proxy |
| M-28 | Medium   | fixed | DCR endpoint has no request-body size limit |
| M-29 | Medium   | fixed | CIMD does not validate `redirect_uris` / `logo_uri` content |
| M-30 | Medium   | fixed | Logs tool uses `ParseDockerLogs` instead of `ParseDockerLogsWithIdleCancel` |
| M-31 | Medium   | fixed | Consent page lacks `Cache-Control: no-store` |
| M-32 | Medium   | fixed | PRM / AS-metadata handlers can double-write on encode error |
| M-33 | Low      | fixed | JWT `alg` header not asserted on verify (defense-in-depth) |
| M-34 | Low      | fixed | `DeriveServiceState` ignores `UpdateStateRollbackStarted` / `RollbackPaused` |
| M-35 | Low      | fixed | Stack secret redaction split between cache and `cluster.RedactSecrets` |
| M-36 | Low      | fixed | `HandleRevoke` is a no-op for access tokens (undocumented) |
| M-37 | Low      | fixed | `update_service_image` does not validate the `image` string |
| M-38 | Low      | fixed | Tier-1/2 write tools missing `readOnlyHint:false` / `idempotentHint` annotations |
| M-39 | Low      | fixed | `SegmentPrefixMatch` allocates memo map per call (hot path) — TODO marker added pending benchmark |
| M-40 | Low      | fixed | Stale doc comment in `internal/cluster/state.go` |
| M-41 | Medium   | fixed | DCR rate-limit bucket map grows unbounded |
| M-42 | Medium   | fixed | `update_service_env` / `update_service_labels` / `update_node_labels` lost-update window |
| M-43 | Low      | fixed | `AuthCodeStore.Issue` for-range variable shadows outer `h` (readability landmine) |
| M-44 | Low      | fixed | `CIMDFetcher.httpClient()` rebuilds transport per fetch (no connection pooling) |
| M-45 | Low      | fixed | AS metadata omits `revocation_endpoint_auth_methods_supported` (RFC 7009 §2) |

---

## Critical — ACL & security correctness

These materially break authorization or violate a security RFC. Land before merging the branch.

### M-15 — `get_logs` MCP tool bypasses ACL on the target service

- **File:** `internal/mcp/tools.go:510-520`
- **Symptom:** `toolGetLogs` calls `readServiceLogsImpl` directly with no `s.checkRead(ctx, "service", …)`. The resource path (`cetacean://services/{id}/logs`) is gated via `lookupResource` → `checkRead`, but the tool path is unguarded. A caller denied `service:production-db` can still fetch its logs through `tools/call get_logs`.
- **Fix:** at the top of `toolGetLogs`, resolve the service name from the cache (mirroring `checkServiceWrite` at `tools.go:475`) and call `s.checkRead(ctx, "service", name)` before invoking `readServiceLogsImpl`. Add a test case to `tools_test.go`.

### M-16 — `remove_config` / `remove_secret` / `remove_network` ACL keys use IDs instead of names

- **File:** `internal/mcp/tools.go:402, 414, 426`
- **Symptom:** these handlers use `s.aclChecker("<type>")`, which forwards the raw Docker ID from the tool args as the ACL resource name. ACL policies are written against names (`secret:db-password`), so `secret:<id>` never matches and either always passes (no-policy / default-allow) or always denies. The result is silent ACL bypass on a tier-3 destructive operation.
- **Fix:** add `checkConfigWrite`, `checkSecretWrite`, `checkNetworkWrite` helpers analogous to `checkServiceWrite` (`tools.go:475-481`) that resolve `Spec.Name` / `Name` from the cache before calling `checkWrite`. Wire them into the three `remove_*` handlers. Add tests asserting the resolved-name key reaches the ACL evaluator.

### M-17 — CIMD SSRF guard has a DNS-rebinding TOCTOU window

- **File:** `internal/mcp/oauth/cimd.go:159-177` (pre-flight `checkSSRF`) and `:125-141` (`CheckRedirect`)
- **Symptom:** `checkSSRF` calls `net.LookupIP`; the subsequent `httpClient().Do(req)` triggers a second DNS resolution inside `net/http`'s transport. An attacker controlling DNS for their `client_id` host can return a public IP on lookup #1 (passes the blocklist) and `169.254.169.254` / `10.x` on lookup #2 (the actual connect). The redirect hook repeats the same two-phase pattern.
- **Fix:** replace the pre-flight `checkSSRF` with a custom `http.Transport.DialContext` that resolves to a concrete `net.IPAddr`, validates that exact IP against the blocklist, then dials it directly (preserve the original `Host` header for SNI). Keep the `CheckRedirect` URL-structure validation as a belt-and-braces check. Add a test that points the CIMD URL at a server returning a Location with a private hostname to cover the redirect path.

### M-18 — PKCE `code_verifier` length not enforced (RFC 7636 §4.1)

- **File:** `internal/mcp/oauth/server.go:215` → `verifySHA256Challenge` at `:300`
- **Symptom:** `verifySHA256Challenge` only rejects empty strings. A one-character verifier is accepted, which defeats the point of PKCE for a passive observer who sees the 43-character base64url challenge (it can be brute-forced trivially). RFC 7636 mandates 43–128 characters from the unreserved set `[A-Za-z0-9-._~]`.
- **Fix:** before calling `verifySHA256Challenge`, validate `len(codeVerifier)` is in `[43, 128]` and that every byte is in the unreserved alphabet. Reject with `invalid_grant`. Add tests for the boundary cases (42, 43, 128, 129) and an out-of-alphabet character.

## High — spec or behavioural deviations

### M-19 — Refresh-token grant lifetime resets on every rotation

- **File:** `internal/mcp/oauth/store.go:252-254`
- **Symptom:** `Rotate` writes `expiresAt: time.Now().Add(ttl)` for the new token. A client that refreshes daily holds the grant perpetually. The design doc says "Default 30-day expiry", which most readers interpret as grant-family expiry, not per-token sliding expiry.
- **Fix:** add `grantExpiresAt time.Time` to `refreshTokenEntry`, set it once in `Issue`, carry it forward unchanged in `Rotate`; the per-token `expiresAt` can remain but should be `min(now+ttl, grantExpiresAt)`. Alternatively: explicitly document sliding refresh in the design doc and leave the code unchanged. **Decide intent first** before coding the fix.

### M-20 — Refresh-token resource mismatch revokes the family on a client typo

- **File:** `internal/mcp/oauth/server.go:253-278`
- **Symptom:** `Rotate` runs *before* `ValidateResourceIndicator` and the bound-resource comparison. On any resource mismatch — including a benign client typo — the entire grant family is burned and the user must reauthorize.
- **Fix:** move resource-indicator validation and the bound-resource check ahead of the `Rotate` call. Only invoke `RevokeGrant` when `Rotate` itself returns `Theft: true` (which it already handles internally) or via an explicit revoke endpoint.

### M-21 — `tools/list` not filtered by per-identity ACL

- **File:** `internal/mcp/server.go:250-252`
- **Symptom:** `filterToolsForIdentity` is a pass-through. The spec § Per-Request Tool Filtering explicitly requires per-identity filtering; the call-time fallback was meant for the case where mcp-go lacks a hook, but the hook exists (`WithToolFilter`, wired at line 130). An identity with zero write grants still sees `scale_service`, `remove_service`, etc. in their catalog.
- **Fix:** for each registered tool, evaluate the ACL predicate for the tool's primary resource type against the request's identity and drop tools that can never pass. This is advisory (call-time ACL still enforces); the goal is a truthful capability list. Add a test asserting an identity with read-only grants gets a tool list containing only `search` and `get_logs`.

### M-22 — `notifications/resources/list_changed` broadcast with no ACL check

- **File:** `internal/mcp/notifications.go:252`
- **Symptom:** every session, including ones with zero subscriptions and zero read grants, receives `list_changed` on any create/remove. The spec § Subscriptions says "ACL-filtered per-notification". Leaks activity timing across tenancy boundaries.
- **Fix:** before broadcasting, filter the recipient set to sessions whose identity can read at least one resource of the affected type. The check has to be fast — consider caching a "can read any of type T" decision per session, invalidated when ACL hot-reloads.

### M-23 — Stack and volume detail ACL check runs before cache lookup → existence leak

- **File:** `internal/mcp/resources.go:203-211` (stacks) and `:255-263` (volumes)
- **Symptom:** every other resource type does cache lookup first and then `checkRead`. Stacks and volumes do the opposite, so an unauthorized caller distinguishes "exists but denied" from "does not exist" by error class.
- **Fix:** swap the order — `GetStackDetail` / `GetVolume` first, return `notFound` when absent, then `checkRead`. Match the pattern used for services/nodes/configs/secrets/networks.

### M-24 — `remove_task` ACL diverges from REST

- **File:** `internal/mcp/tools.go:214`
- **Symptom:** REST routes task DELETE ACL to the parent service (`internal/api/write_middleware.go:98-111`, key `service:<name>`). MCP's `remove_task` keys on `task:<id>`. A policy of `service:web write` allows REST removals of `web`'s tasks but not MCP ones — and vice versa for a `task:*` policy. Same operation, two ACL semantics.
- **Fix:** add `checkTaskWrite` that resolves the parent service via `cache.GetTask(id)` → `cache.GetService(task.ServiceID)` and calls `s.checkWrite(ctx, "service", svc.Spec.Name)`. Wire into the `remove_task` handler.

## Medium

### M-25 — `discardingResponseWriter.Header()` returns a fresh map per call

- **File:** `internal/mcp/server.go:240`
- **Symptom:** `Header()` returns `http.Header{}` each call, so writes are silently dropped. The current call site (cert auth bypass) doesn't write, but the design is fragile: any future provider added to `AuthBypass` that calls `w.Header().Set(...)` followed by `w.Header().Get(...)` will see empty state.
- **Fix:** make the receiver a non-empty struct holding a persistent `http.Header`:

  ```go
  type discardingResponseWriter struct{ h http.Header }

  func newDiscardingResponseWriter() discardingResponseWriter {
      return discardingResponseWriter{h: make(http.Header)}
  }

  func (d discardingResponseWriter) Header() http.Header { return d.h }
  ```

### M-26 — `update_service_env` / `update_service_labels` reject `null` (breaks merge-patch delete)

- **File:** `internal/mcp/tools.go:869` (`requireStringMap`)
- **Symptom:** REST accepts `{"FOO": null}` to delete an env var (JSON Merge Patch semantics); MCP rejects it with `must be a string`. Agents following the documented merge-patch contract cannot delete keys via MCP.
- **Fix:** accept `nil` values in `requireStringMap` and pass them through as deletions. Likely needs the downstream writer signature to be `map[string]*string` or a `(set, delete)` tuple — match how `internal/api/jsonpatch.go` already handles it.

### M-27 — DCR rate-limit keyed on `r.RemoteAddr` — bypassed behind a reverse proxy

- **File:** `internal/mcp/oauth/dcr.go:150-154`
- **Symptom:** behind a reverse proxy (the documented deployment per `CETACEAN_MCP_ISSUER`'s description), every client appears as the proxy IP and shares one bucket.
- **Fix:** reuse the `CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES` infrastructure: when the immediate peer is a trusted proxy, peel the outermost `X-Forwarded-For` entry. Document the dependency on trusted-proxy config.

### M-28 — DCR endpoint has no request-body size limit

- **File:** `internal/mcp/oauth/dcr.go:161-165`
- **Symptom:** `json.NewDecoder(r.Body).Decode(&req)` will buffer arbitrarily large bodies before erroring. Public endpoint, trivial DoS.
- **Fix:** wrap `r.Body = http.MaxBytesReader(w, r.Body, 64*1024)` before decoding. Mirror in any other JSON-accepting MCP endpoint that isn't already capped.

### M-29 — CIMD does not validate `redirect_uris` / `logo_uri` content

- **File:** `internal/mcp/oauth/cimd.go`
- **Symptom:** the document's `redirect_uris` are accepted verbatim and compared by string equality. A CIMD doc advertising `"javascript:..."` would pass `HasRedirectURI`. The consent screen also displays the URI to the user. DCR validates URIs via `isValidRedirectURI`; CIMD should run the same check at fetch time. Same for `logo_uri` (https-only) if it's ever rendered.
- **Fix:** in `CIMDFetcher.Fetch` after JSON-decode, run `isValidRedirectURI` (or its CIMD equivalent — loopback rules may differ) against every entry in `RedirectURIs`. Reject `logo_uri` schemes other than `https`. Add a test with a malicious document.

### M-30 — Logs tool uses `ParseDockerLogs` instead of `ParseDockerLogsWithIdleCancel`

- **File:** `internal/mcp/logs.go:67-74`
- **Symptom:** Docker's `ServiceLogs` with `Follow=false` doesn't always close the stream cleanly. The REST log handler uses the idle-cancel variant; MCP blocks up to `logFetchTimeout` (5s) on every read where the stream doesn't self-close.
- **Fix:** switch to `api.ParseDockerLogsWithIdleCancel` (the same function the REST handler uses) with a short idle timeout (e.g. 250ms).

### M-31 — Consent page lacks `Cache-Control: no-store`

- **File:** `internal/mcp/oauth/consent.go:127-130` (`setConsentHeaders`)
- **Symptom:** the rendered consent page carries CSRF token, `state`, `code_challenge`, `redirect_uri`, and authenticated user identity in hidden fields. A shared proxy cache or browser back-button cache can replay it to a different user. Token responses already set `no-store`; the consent page should too.
- **Fix:** add `w.Header().Set("Cache-Control", "no-store")` inside `setConsentHeaders` (covers both the consent page and the error page that shares the helper).

### M-32 — PRM / AS-metadata handlers can double-write on encode error

- **File:** `internal/mcp/oauth/server.go:132-136`, `prm.go:29-33`, `dcr.go:230-235`
- **Symptom:** `json.NewEncoder(w).Encode(doc)` writes headers and partial body, then `http.Error` appends a second response on failure → corrupted output.
- **Fix:** `b, err := json.Marshal(doc); if err != nil { http.Error(w, ...) ; return } ; w.Header().Set(...) ; w.Write(b)`.

## Low

### M-33 — JWT `alg` header not asserted on verify

- **File:** `internal/mcp/oauth/jwt.go:96-110`
- **Symptom:** verify recomputes HMAC-SHA256 regardless of what the token's `alg` claims. In practice this prevents `alg=none` forgeries (the empty signature mismatches the recomputed HMAC), but explicitly asserting `alg=="HS256"` after decoding `parts[0]` adds defence-in-depth and makes the contract self-documenting for future maintainers who might add additional algorithms.
- **Fix:** decode and unmarshal `parts[0]`, return `ErrMalformedToken` if `alg != "HS256"` or `typ != "JWT"`.

### M-34 — `DeriveServiceState` ignores `UpdateStateRollbackStarted` / `RollbackPaused`

- **File:** `internal/cluster/state.go:11-13`
- **Symptom:** rollbacks-in-progress show as `running` or `pending` in both REST search and MCP search because only `UpdateStateUpdating` triggers the `updating` branch.
- **Fix:** extend the `if` to a `switch` covering `UpdateStateUpdating`, `UpdateStateRollbackStarted`, `UpdateStateRollbackPaused`. Add test cases.

### M-35 — Stack secret redaction split between cache and `cluster.RedactSecrets`

- **File:** `internal/cache/cache.go:643-645`
- **Symptom:** `GetStackDetail` does inline `Spec.Data = nil` redaction; everywhere else routes through `cluster.RedactSecrets`. Two sites of truth for a security contract. The MCP `stacks/{name}` path silently depends on the cache's inline redaction; refactor the cache and MCP leaks secret data without compile-time warning.
- **Fix:** remove the inline loop in `cache.go:643-645`; apply `cluster.RedactSecrets` at the transport boundaries (REST `HandleGetStack`, MCP `lookupResource`). Unit test that the cache's `GetStackDetail` *returns* secret data (so future leaks via copy-paste are visible) and that both transports redact at the edge.

### M-36 — `HandleRevoke` is a no-op for access tokens (undocumented)

- **File:** `internal/mcp/oauth/server.go HandleRevoke`
- **Symptom:** RFC 7009 mandates 200 OK regardless, so this is spec-compliant — but a revoked access-token JWT continues to validate until `exp` (default 1h). Worth a comment in the handler explaining the limitation. If we ever care about immediate revocation, we'd need a JTI denylist with TTL matching `AccessTokenTTL`.
- **Fix:** add a code comment documenting the limitation; optionally add a `token_type_hint=access_token` early return that doesn't even hash.

### M-37 — `update_service_image` does not validate the `image` string

- **File:** `internal/mcp/tools.go:548-572`
- **Symptom:** an LLM passing `""` or whitespace reaches Docker. `scale_service` validates `replicas >= 0`; image deserves the same.
- **Fix:** `image = strings.TrimSpace(image); if image == "" { return "", fmt.Errorf("image must not be empty") }`.

### M-38 — Tier-1/2 write tools missing `readOnlyHint:false` / `idempotentHint`

- **File:** `internal/mcp/tools.go:163-440`
- **Symptom:** spec § Tool Behavior calls these out. Currently only tier-3 removals carry `destructiveHint:true`. MCP clients use these hints to render confirmation dialogs; without them, write tools may be presented as innocuous.
- **Fix:** add `mcplib.WithReadOnlyHintAnnotation(false)` to every tier-1/2 write tool registration; add `WithIdempotentHintAnnotation(true)` to the idempotent ones (`scale_service`, all the `update_service_*` tools).

### M-39 — `SegmentPrefixMatch` allocates memo map per call (hot path)

- **File:** `internal/cluster/search.go:448-449`
- **Symptom:** `memo := map[key]bool{}` allocates per call; called for every label key/value on every resource in parallel search goroutines. Real GC pressure during large-cluster searches.
- **Fix:** benchmark first. If hot, replace recursive memoised backtracking with an iterative DP using a preallocated `[]bool`. Mark as a TODO for now.

### M-40 — Stale doc comment in `internal/cluster/state.go`

- **File:** `internal/cluster/state.go:6`
- **Symptom:** comment says "Mirrors the inlined logic in `internal/api/search_handlers.go`" — that file no longer has inline logic; it calls `DeriveServiceState`. Confuses future readers.
- **Fix:** rewrite as "Used by both REST (`internal/api/search_handlers.go`) and MCP (`internal/mcp/tools.go`) search to report identical service state."

---

## Round 3 — found during M-15..M-40 verification

A fresh review pass after commit `2aeaf6e` landed turned up five additional issues. Severities are calibrated to the round-2 scale.

### M-41 — DCR rate-limit bucket map grows unbounded

- **File:** `internal/mcp/oauth/dcr.go:93-113`
- **Symptom:** `checkRateLimit` creates a `*ipBucket` entry per source IP and only ever replaces it in-place when the entry exists; the map is never swept. A long-running server hit from many IPs (background scanners, legitimate churn behind NAT, or rotating proxies) accumulates entries indefinitely. Each bucket is tiny, but there's no upper bound — pair with `dcrMaxBodyBytes` being public and you have a slow memory leak that can be driven externally.
- **Fix:** sweep expired buckets inside `checkRateLimit` while the lock is already held (mirror the inline sweep `AuthCodeStore.Issue` already does). The sweep is `O(n)` per call but the map stays bounded to the active-IP working set.

### M-42 — env / labels merge-patch lost-update window

- **File:** `internal/mcp/tools.go:960-975` (env), `:994-1008` (service labels), `:1025-1041` (node labels); `internal/api/write_helpers.go:300+`, `internal/api/write_labels.go:81-92` (REST counterparts); `internal/docker/client.go:647-718` (writer).
- **Symptom:** the merge-patch handlers read the current env/labels map from the in-memory cache, compute `merged = applyMergePatch(current, patch)`, then call `wc.UpdateServiceEnv(ctx, id, merged)`. The Docker writer does a fresh `ServiceInspectWithRaw`, overwrites the entire env slice with `merged`, then `ServiceUpdate`s with the freshly-inspected `Version`. If another writer (REST agent, second MCP agent, `docker service update` from the CLI) mutates the same field between the handler's cache read and the writer's inspect, that change is silently clobbered because the merge happened against the stale snapshot. Docker's `Version`-based concurrency check doesn't help — the version it compares against is the one the writer just inspected, not the one the handler observed.
- **Fix:** change the writer signature to accept a patch directly (`patch map[string]*string` where `nil` deletes, non-nil sets) and apply the merge against the *fresh* inspect inside the writer. Handlers stop pre-merging — they just hand the parsed patch through. Closes the lost-update window without adding a new round-trip.

### M-43 — `AuthCodeStore.Issue` for-range variable shadows outer hash

- **File:** `internal/mcp/oauth/store.go:64-82`
- **Symptom:** the outer `h := hashToken(raw)` and the inner `for h, entry := range s.codes` use the same name. Go's loop-variable scoping does preserve the outer `h` after the loop exits (verified in round 2), so the code is correct — but two independent reviewers tripped on it and assumed a bug. Defuse the landmine.
- **Fix:** rename the inner loop var (`for hash, entry := range s.codes`). Pure cosmetic; no behaviour change.

### M-44 — `CIMDFetcher.httpClient()` rebuilds transport per fetch

- **File:** `internal/mcp/oauth/cimd.go:133-155`
- **Symptom:** every call to `Fetch` invokes `httpClient()`, which constructs a fresh `http.Client` and (when `f.Client == nil`) a fresh `http.Transport` with its own connection pool. No connections are reused across fetches — every CIMD fetch starts with a cold dial and TLS handshake. The pool's `MaxIdleConns: 16` is wasted.
- **Fix:** build the transport (and the `CheckRedirect` closure) once per fetcher and cache it on the struct, behind a `sync.Once`. Validate-once instead of allocate-per-call.

### M-45 — AS metadata omits `revocation_endpoint_auth_methods_supported`

- **File:** `internal/mcp/oauth/server.go:103-130`
- **Symptom:** the RFC 8414 metadata advertises a `revocation_endpoint` but doesn't say which auth methods it accepts. RFC 7009 §2 says servers "SHOULD provide" `revocation_endpoint_auth_methods_supported`; absence forces strict clients to assume `client_secret_basic` is required, which we don't accept. Same omission as `token_endpoint_auth_methods_supported` was before it was added.
- **Fix:** add the field to `asMetadata`, populate with `["none"]` (matching our token endpoint policy).

---

## Notes from the round 3 implementation

- **M-42 (lost-update)** turned out to touch the writer interface contract. Rather than thread a per-key patch through (which would need separate set/delete signatures across services/configs/secrets/nodes), the three label/env writer methods now accept a `MapMutator` — a `func(current map[string]string) (map[string]string, error)`. The handler parses the JSON Patch / Merge Patch once and hands the writer a closure that knows how to apply it; the writer invokes the closure against the freshly-inspected spec. The same mutator type is reused for `UpdateConfigLabels` / `UpdateSecretLabels` even though those weren't called out in the symptom — REST patched configs/secrets through the same `handlePatchLabels` helper and inherited the same race. Patch-application errors (RFC 6902 `test` mismatches, missing keys on `replace`/`remove`) now surface from the writer; a new `errPatchApply` sentinel plus `isPatchApplyError` in the REST layer keeps the 400/409 mapping intact. Test mocks gained a `simulatedEnv` / `simulatedLabels` field to stand in for the writer's "fresh inspect"; existing assertions still match the resolved map handed to their `*Fn` callbacks.
- **M-44 (transport reuse)** was simpler than first sketched: a single `sync.Once` builds the `*http.Client` (with its SSRF-aware Transport and CheckRedirect closure) on first fetch and reuses it thereafter. Mutating `Client` or `AllowLoopback` after the first fetch has no effect — the once-only init is documented inline.
- **M-43 (loop var)** is a pure cosmetic rename in `AuthCodeStore.Issue`. Round 2 confirmed there was no behaviour bug; the change just removes the trip wire that has now caught two independent reviewers.
- **M-41 (DCR sweep)** sweeps inline under the existing lock — adding a background goroutine for this would dwarf the cost it eliminates. The map is now bounded by the active-IP working set in any rate-limit window, not by all-time unique sources.

---

## False positives caught during verification

For the historical record — these were flagged with high confidence by the parallel reviewers but found to be incorrect:

- **`AuthCodeStore.Issue` for-loop shadowing.** The reviewer claimed `for h, entry := range s.codes` overwrites the outer `h := hashToken(raw)`, so `s.codes[h] = ...` stores under the wrong key. Go scopes for-loop variables to the loop's implicit block; the outer `h` is preserved after the loop exits. Verified at `internal/mcp/oauth/store.go:64-82`. Stylistic nit — renaming the loop var would prevent future readers from making the same mistake — but no behavioural bug.
- **Double-write findings (M-32) are real**, but in practice `json.Marshal` succeeds for these fixed-shape structs; the impact is theoretical. Kept in the list as a correctness hygiene fix, not a security issue.
- **CIMD client-id mismatch race (`putCached` after concurrent fetches).** Reviewer flagged a singleflight gap. Investigated: same-origin concurrent fetches converge on the same document; second-writer-wins is correct behaviour, not a vulnerability.

---

## Notes from implementation

A few choices and observations worth recording for whoever picks this up next:

- **M-19 (refresh-token grant lifetime)** was resolved in favour of the *absolute grant-family expiry* reading — `grantExpiresAt` is set once at `Issue` and carried unchanged through every `Rotate`; per-token expiry clamps to it. This is the stricter and more security-friendly default; sliding refresh is the alternative semantics that some implementations prefer, but the design doc's "Default 30-day expiry" most naturally maps to an absolute cap.
- **M-26 (merge-patch deletes)** was not just a `null`-acceptance fix: the previous code was actually doing a full replace of the env / labels map despite advertising "merge patch" in tool descriptions. The MCP handlers now read the current spec from the cache, apply the patch (string sets, `null` deletes), and pass the full merged map to the underlying Docker writer — matching how REST's `HandlePatchServiceEnv` composes the call.
- **M-27 (DCR rate-limit XFF)** is already handled at the router level by `internal/api/realip.go`, which rewrites `r.RemoteAddr` from the rightmost non-trusted `X-Forwarded-For` entry when `server.trusted_proxies` is configured. A comment was added at the DCR handler so future readers don't reimplement it; the operational dependency stays the same as for the rest of the application's IP-aware logic (auth headers, CORS).
- **M-35 (stack secret redaction)** was resolved by removing the redundant inline loop in `GetStackDetail`. The cache's secret `onSet` hook already nilifies `Spec.Data` on every insertion, so the data is gone before `GetStackDetail` ever reads it. The deeper refactor proposed in the original write-up (cache holds raw data, transports redact at the edge) would invert the current security contract and was not adopted.
- **M-39 (SegmentPrefixMatch hot-path allocations)** is annotated with a `TODO(perf)` and left in place. The fix needs a benchmark to know whether the change is worth the readability cost — flagging it without measurement risked introducing a subtle correctness bug in search ranking for marginal gain.
- **M-17 (CIMD SSRF DialContext)** changed the relationship between `CIMDFetcher.Client` and the SSRF guard. The default (production) path now builds an `http.Transport` whose `DialContext` performs DNS + IP validation atomically. When a caller injects a `Client` (the tests use `httptest.Server.Client()`), the transport is cloned and decorated with the same `DialContext`; tests that need to reach a loopback `httptest` server continue to set `AllowLoopback: true`. Pre-flight `checkSSRF` has been deleted to enforce a single source of truth.
