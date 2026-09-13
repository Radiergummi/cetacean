# API Access Tokens from the OAuth Server

**Date:** 2026-09-13
**Status:** Investigation. Phase 0 of `2026-09-13-native-apple-client-design.md`, written up
separately because it stands on its own: any CLI, script or third-party client wants it whether or
not a native app is ever built.

## Summary

Teach the existing OAuth 2.1 authorization server to issue access tokens for the REST API as a
second protected resource, and teach the auth middleware to accept them. The API gains a credential
a client can obtain unattended, on every auth provider, with no operator configuration.

The two resources stay separate: a token minted for MCP must not open the REST write surface, and
vice versa. That separation is the whole reason to do this through resource indicators rather than
by widening the existing audience.

## What exists today

`internal/mcp/oauth` is a complete AS: RFC 7591 DCR, CIMD, PKCE S256 (required, with verifier
length and alphabet enforced), authorization codes with a 60 s TTL, rotating refresh tokens with
theft detection, a consent screen with remembered approvals, RFC 8707 resource indicators, RFC 9068
`at+jwt` access tokens signed ES256 with a published JWK Set, RFC 9728 protected resource metadata,
and durable state under `{data_dir}/mcp-tokens.json`. Defaults: access 1 h, refresh 30 d, consent
90 d.

Five facts about it shape everything below.

- **The audience is a constant.** `TokenIssuer.Audience` is set once from `ServerConfig.MCPResource`
  and `IssueAccessToken` stamps it on every token; `VerifyAccessToken` compares for exact equality.
  The resource indicator *is* validated and *is* stored on the authorization code and the refresh
  token, but nothing reflects it into `aud` — with one resource there was nothing to reflect.
- **There are no scopes.** `AccessTokenClaims` is `sub`, `groups`, `client_id`. Authorization comes
  entirely from the upstream identity reaching the ACL. A token therefore carries exactly its
  user's access, no more and no less.
- **The AS exists only when MCP does.** `setupMCP` returns early on `!cfg.MCP.Enabled`, and
  constructs the server only when `authMode != "none"`. Its knobs live in `config.MCPConfig`.
- **`internal/api` does not import `internal/mcp`.** Deliberate, and stated at `carriesItsOwnProof`
  in `internal/api/csrf.go`. The MCP handler and the OAuth routes both reach the router as
  injected values from `main.go`.
- **The resource server is `internal/mcp`, not the AS.** `Server.bearerAuth` verifies the token and
  builds an `auth.Identity` from the claims. Nothing else in the tree verifies a Cetacean-issued
  token.

## Decisions

### 1. Resource identifiers, and where the PRM documents live

This is the part that actually needs thought, because RFC 9728 derives the metadata URL from the
resource identifier's path, and today's single document sits at the root-path location while
describing a resource that has a path.

Today:

| | |
|---|---|
| Resource identifier | `{issuer}{base_path}/mcp` |
| PRM served at | `{issuer}{base_path}/.well-known/oauth-protected-resource` |
| `WWW-Authenticate` points at | that same URL |

RFC 9728 §3.1 builds the metadata URL by inserting `/.well-known/oauth-protected-resource` after
the host and appending the resource's path — so the MCP resource's metadata belongs at
`/.well-known/oauth-protected-resource/mcp`, and the root-path location belongs to a resource
identifier with no path.

**Recommendation.** The API's resource identifier is the deployment root, `{issuer}{base_path}`,
and takes the root-path PRM. MCP keeps its identifier and moves to the path-based one:

| Resource | Identifier | PRM |
|---|---|---|
| REST API | `{issuer}{base_path}` | `{base_path}/.well-known/oauth-protected-resource` |
| MCP | `{issuer}{base_path}/mcp` | `{base_path}/.well-known/oauth-protected-resource/mcp` |

Each `WWW-Authenticate` names its own document, so a client that discovers the way RFC 9728 and the
MCP authorization spec tell it to — read the 401, follow `resource_metadata` — is unaffected by the
move. A client that hardcoded the root path breaks. That is wire-visible and owes a changelog entry
and a line in `docs/mcp.md`; it cannot be softened by serving both, because one URL can describe
only one resource.

**The root identifier does not imply containment.** A reader will assume a token for
`https://host/` covers `https://host/mcp` because one path is under the other. It does not, and must
not: audience comparison is exact string equality, so the two are simply different audiences. Say so
in the doc comment, because the alternative reading is the audience-confusion attack the resource
indicator exists to prevent — a user who grants an AI agent MCP access has not granted it
`DELETE /services/{id}`.

Rejected: giving the API the identifier `{issuer}{base_path}/api`. It collides with the OpenAPI
endpoint, which the current PRM already names as `resource_documentation`, and it wrongly suggests
the token is scoped to that path.

### 2. `aud` comes from the bound resource

`IssueAccessToken` takes the audience per call, from `codeData.Resource` / `RefreshTokenData.Resource`
— the value already validated and stored. `VerifyAccessToken` takes the expected audience from its
caller. `TokenIssuer.Audience` stops being a field, or becomes a default for the no-indicator case.

`ValidateResourceIndicator` then needs a set rather than one expected value: empty and not required
resolves to a configured default (the API resource, which is what an unmodified client should get),
empty and required is an error as now, non-empty must match one of the known identifiers exactly.
Keep the exactness — a prefix match here would reintroduce containment.

This is the change that makes a mis-targeted token fail loudly: presenting an MCP-audience token to
the REST API produces `ErrAudienceMismatch`, which the middleware must answer as a 401
`invalid_token` naming the *API's* PRM, not as a fall-through.

### 3. Verification runs in `internal/auth`, behind an interface

`internal/api` cannot import `internal/mcp`, and putting the verifier in `internal/auth` while the
OAuth package imports `internal/auth` for `IdentityFromContext` would be a cycle.

Use the pattern the codebase already uses twice (`auth.SetErrorWriter`, `api.RouterConfig.MCPHandler`):
`internal/auth` declares the narrow interface it needs, `main.go` wires the implementation.

```go
// In internal/auth.
type TokenVerifier interface {
    // VerifyAPIToken returns the identity a Cetacean-issued API token carries.
    VerifyAPIToken(token string) (*Identity, error)
}
```

`auth.Middleware` takes an optional verifier and consults it before the provider:

1. No `Authorization: Bearer` → provider path, unchanged.
2. Bearer present, verifier absent → provider path, unchanged.
3. Bearer present and the token verifies → identity from claims, proceed.
4. Bearer present, token is ours and does not verify → **401, final.** Never fall through, and
   never the OIDC provider's `text/html` redirect-to-login branch: a native client must receive a
   401 with `WWW-Authenticate`, not a 302.
5. Bearer present and the token is not ours → provider path, so an OIDC ID token keeps working.

**4 and 5 are the fiddly bit**, and the only real trap in this design. Two bearer dialects share one
header: Cetacean's `at+jwt` and, in `oidc` mode, an IdP-issued ID token. The discriminator is the
issuer, not the outcome of verification — so `ErrIssuerMismatch` (and a malformed-JWT error, which
is what an opaque IdP token looks like) means "not mine, fall through", while a bad signature, an
expired token or an audience mismatch under *our* `iss` is final. Getting this backwards either
leaks a fall-through for forged Cetacean tokens or breaks `oidc` bearer auth; it wants a table-driven
test over every error `VerifyAccessToken` can return, not a happy-path one.

Whether `internal/mcp/oauth` moves to `internal/oauth` is a consequence of decision 6, not of this
one — the interface decouples either way.

### 4. The identity in a token must be the identity from the provider

`bearerAuth` builds `&auth.Identity{Subject, Groups, Provider}` today. `DisplayName` and `Email` are
dropped, because the claims never carried them.

That is not cosmetic. `acl.matchAudience` resolves `user:pattern` against **Subject or Email**, so a
policy written the way `docs/authorization.md` documents it —

```yaml
- resources: ["stack:public-*"]
  audience: ["user:*@example.com"]
  permissions: ["read"]
```

— matches a browser session and silently fails to match a token from the same person, unless their
IdP `sub` happens to be an address. The grant does not apply, the ACL is default-deny, and the
client sees an empty cluster with no error. **This affects MCP today**, not just a future API
token; nothing in `internal/mcp` or `internal/acl` appears to pin the combination.

So: add `email` and `name` to `AccessTokenClaims` and reconstruct the full identity. `Raw` stays out
— it is already stripped from the session cookie to keep it under 4 KiB, and a JWT in an
`Authorization` header wants the same restraint. Confirm the current behaviour with a test first, so
the fix lands as a fix rather than as an assumption.

### 5. No scopes in this change, but reserve the tier cap

Tempting to add `scope` and let a device hold less than its user. Resisted, for now:

- The ACL is the project's authorization model, and a second one layered over it — scope ∩ grant —
  is a new way for access to be surprising. `Allow` already has to be the single answer.
- What a device actually needs narrowing on is *how destructive* it may be, and there is already a
  precedent for that axis: `mcp.operations_level` caps a transport below the global tier.

**Recommendation:** mirror it as an operations-level cap for token-authenticated callers
(`api.token.operations_level` or similar, inheriting the global by default). It composes with
everything — `requireLevel` already enforces it, `Allow` already reports it, so the native app's
controls gate themselves with no client-side logic. A read-only token is then `level 0`, which is
exactly the credential a widget or a Shortcut should hold.

Leave room for `scope` in the claims and the AS metadata so adding it later is not a token-format
migration, but do not implement it here.

### 6. Extract the package, as its own change, first

Yes — and it is less work than it looks, because **the package does not depend on `internal/mcp`
today**. Its only internal imports are `internal/auth` (for `IdentityFromContext`) and
`internal/config`. Nothing in those 3,495 non-test lines reaches into MCP; it is MCP's by naming and
by configuration, not by coupling. So this is a rename plus a config split, not an untangling.

The consumer surface is two methods. `internal/mcp/server.go` holds an `*oauth.Server` and calls
`VerifyAccessToken` and `WriteUnauthorized`, both from `bearerAuth`. That is small enough that
`internal/mcp` should take an interface it declares itself and stop importing the package at all —
the same shape the API side gets from decision 3, and it makes the MCP tests able to fake a verifier.

What is actually MCP-specific, in full:

| Where | Today | Becomes |
|---|---|---|
| `ServerConfig.MCPResource` | one resource string | a set of resource identifiers |
| `ServerConfig.MCP config.MCPConfig` | the AS reads `DCREnabled`, `DCRMaxClients`, `DCRRateLimit`, `CIMDEnabled`, `ConsentTTL`, `AccessTokenTTL`, `RefreshTokenTTL`, `RequireResourceIndicator` | `config.OAuthConfig` — every one of those is an AS setting that happens to be spelled `mcp.*` |
| `prm.go` | one document, one resource | one per resource, at its RFC 9728 path |
| `WriteUnauthorized` | `realm="mcp"` hardcoded | realm per resource |
| `consent.go` | `csrfCookieName = "mcp_csrf_nonce"` | rename is free; it only costs a re-prompt to anyone mid-flow across the deploy |
| `persist.go` + `main.go` | `{data_dir}/mcp-tokens.json` | `oauth-tokens.json`, falling back to the old name once |
| log and panic strings | "MCP OAuth …", `"mcp/oauth: …"` | cosmetic |
| comments in `dcr.go`, `store.go` | "what MCP clients overwhelmingly are", "the MCP endpoint URL" | cosmetic, but they are the ones that would mislead the next reader |

**One thing must not be renamed.** `keys.go` derives every key from the configured root through HKDF
labels spelled `cetacean/mcp/hkdf/v1`, `cetacean/mcp/csrf/v1` and `cetacean/mcp/jwt/es256/v1`, and
already carries the comment saying what changing them costs. They are opaque labels, not API, and the
`/mcp/` in them is historical. Renaming them to match the package would invalidate every live access
token, change the JWKS `kid`, and refuse every consent form in flight. Refresh tokens survive — they
are opaque random values stored hashed, independent of the key material — so the blast radius is one
forced refresh per client rather than a re-authorization, but it buys nothing. Leave them, and say in
the comment that they are deliberately stale so nobody tidies them later.

Everything else in the package needs no change at all: `jwt.go`, `jwks.go`, `store.go`,
`consent_store.go`, `cimd.go`, `dcr.go`, `resource_indicator.go` and `persist.go` are already
resource-agnostic.

**Sequence it as two changes.** A pure move first — `internal/mcp/oauth` → `internal/oauth`, the
config type split out with aliases, the interface at the MCP boundary, comments de-MCP'd, and no
behaviour change anywhere, with the existing 4,458 lines of tests moving unmodified as the proof.
Then the two-resource change on top. Done in one commit, the diff that introduces a second audience
is buried in a rename and nobody can review the part that matters.

### 7. The AS has to outlive `mcp.enabled`

Extraction alone does not get there: the AS is *constructed* inside `setupMCP`, which returns
`(nil, nil, noop)` when MCP is off. An operator who wants a native client but no AI agent on their
cluster would still get no tokens. So the lifecycle moves too — construct it in `main.go` beside the
auth provider, gated on its own setting, and pass it to both the router and `setupMCP`.

That is what makes the config split in decision 6 load-bearing rather than cosmetic.
`mcp.signing_key`, `mcp.access_token_ttl`, `mcp.refresh_token_ttl`, `mcp.consent_ttl`, `mcp.dcr_*`,
`mcp.require_resource_indicator` and `mcp.issuer` are documented in `docs/configuration.mdx`, which
is canonical — so they need aliases and a deprecation period, not a rename. An operator running MCP
today must not have to edit their compose file to keep it working.

Keep the `authMode != "none"` guard. In `none` mode the ACL is bypassed and every identity is
`anonymous`; a token would authenticate nobody to nothing, and a consent screen asking `anonymous`
to approve a client is theatre.

## How it interacts with the rest of the system

Mostly: it doesn't, which is the point of landing it in the middleware.

- **ACL and `Allow`.** Both read `auth.Identity` from the request context. A token-derived identity
  is an identity; grants, `requireWriteACL`, the `Allow` header and `/profile` all behave as they do
  for a cookie — *provided* decision 4 lands, which is what makes the two identities equal.
- **Operations tier.** `requireLevel` is unchanged. With decision 5 it gains one more input.
- **Cross-origin protection.** No change needed. `crossOriginProtection` refuses a non-safe request
  only when Fetch Metadata or `Origin` says cross-site; a client sending neither passes, which
  `internal/api/csrf_test.go` pins as "non-browser write is allowed". A native app sends neither.
  A *browser* client holding a token is still subject to the check, which is correct —
  `carriesItsOwnProof` must not grow an entry for the API, or every REST path would lose the
  protection that Tailscale, mTLS and header modes depend on.
- **`isExempt`.** No new entry. `/mcp` is exempt because it authenticates itself; the API's tokens
  are checked *by* the middleware, so the API stays non-exempt. `/oauth/authorize` stays
  non-exempt too — consent must run under a real session, which is precisely how the upstream
  provider's identity gets into the token.
- **`cert` mode.** The consent page needs a browser that can present a client certificate, which an
  in-app web view largely cannot. `mcp.auth_bypass` already exists for exactly this: it lets an
  mTLS client skip the OAuth detour and authenticate directly. A native client on a `cert`-mode
  deployment should use a Keychain identity against the API directly and never touch the AS. Do not
  extend the bypass to the API middleware — the provider already handles it there; the bypass
  exists only because `/mcp` is exempt.
- **SSE.** A stream is authenticated when it opens. There is no max-duration cap on a stream today,
  so one opened with a token valid for another minute outlives the token. That is already true of a
  session cookie, so it is not a regression — but it is worth stating, and a duration cap is the fix
  if it ever matters. `EventSource` cannot set an `Authorization` header; a native client uses
  `URLSession` and can, so this does not constrain the app.
- **Base path.** `issuerID()` is already the single source of truth for the advertised issuer, the
  PRM `authorization_servers` entry and the `iss` claim. The new resource identifier and the new PRM
  path both derive from it, so a base-path deployment needs no special case —
  `internal/mcp/oauth/discovery_basepath_test.go` is the model for the coverage.

## Config surface

One new setting beyond the aliases, plus the tier cap from decision 5:

| Setting | Default | Purpose |
|---|---|---|
| `oauth.enabled` | `true` when `auth.mode != none` | Whether the AS runs at all, independent of `mcp.enabled` |
| `oauth.api_tokens` | `true` | Whether the API is offered as a resource |
| `api.token.operations_level` | inherit | Tier cap for token-authenticated callers |

`oauth.api_tokens = false` is the escape hatch for an operator who wants MCP tokens and nothing
else. It must make the API resource undiscoverable as well as unusable: no root-path PRM, no entry
in the API catalog, and a refusal at the authorize endpoint — not a token that mints and then fails.

## Deliberately not in scope

- **Scopes.** Decision 5.
- **A device list UI.** `/oauth/revoke` and the consent store exist, but nothing surfaces "which
  clients hold a grant" or offers to forget one. Users will expect it the moment a phone holds a
  token. It is a follow-up, and it is a dashboard page plus a store query, not a protocol change.
- **Long-lived personal access tokens.** A flat bearer string would be simpler for scripts than DCR
  plus PKCE, and strictly worse: no rotation, no theft detection, no consent record, no expiry
  unless someone remembers. The refresh token already is the long-lived credential, and it rotates.
- **Accepting MCP tokens on the API or the reverse.** The separation is the feature.

## Test obligations

The ones that would actually catch a mistake here:

- The discriminator table from decision 3: every error `VerifyAccessToken` can return, mapped to
  fall-through or final, with an `oidc`-mode provider behind it so a regression breaks IdP bearer
  auth loudly.
- Audience confusion both ways: an MCP-audience token refused by the API, an API-audience token
  refused by `/mcp`, each with the correct `resource_metadata` in the 401.
- Identity fidelity: the same person, over a cookie and over a token, matching the same
  email-keyed grant and receiving the same `Allow` — the assertion decision 4 exists for.
- Both PRM documents under a base path, and the `WWW-Authenticate` from each transport naming its
  own.
- A restart with the old `mcp-tokens.json` present keeping every grant alive.
