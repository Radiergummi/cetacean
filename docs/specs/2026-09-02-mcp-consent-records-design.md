# MCP consent records

**Issue:** [#160](https://github.com/Radiergummi/cetacean/issues/160)
**Depends on:** [#74](https://github.com/Radiergummi/cetacean/issues/74), merged as `0f71654e`
**Status:** design approved, not implemented

Record a user's consent decision so an already-approved MCP client does not
re-prompt on every authorization request.

## What exists today

`internal/mcp/oauth/consent.go` renders the consent page and handles CSRF (a
nonce cookie plus an HMAC token over `nonce|state`). Nothing writes "user X
approved client Y". `handleAuthorizeGET` validates the request, resolves the
client's metadata, requires an identity from the auth middleware, and then
always renders the form.

Consent is remembered only _implicitly_, by a refresh token existing: a client
holding one never returns to `/oauth/authorize`, so the user is not re-prompted
until the grant expires (720h by default) or is revoked.

## Value, stated honestly

Smaller than it first appears. Now that #74 persists refresh tokens, the consent
page already appears about once a month per client. A consent record turns that
into "once, until revoked". That is a UX improvement, not a durability fix, and
it should not be mistaken for one.

## Decisions

### The fingerprint covers what the user saw

A DCR registration is frozen at registration time, but CIMD — the recommended
path since 2026-07-28 — puts the metadata at a URL the client controls and may
change at any moment, including its `redirect_uris`. A record saying "user
approved `https://foo.example/client.json`" would otherwise auto-approve a later
flow redirecting somewhere the user never saw.

The fingerprint is a domain-separated SHA-256 over the client's `client_name`
and its **sorted** `redirect_uris` — precisely the two things the consent page
displays. A rename, an added URI, a removed URI or an edited URI all re-prompt.
Reordering does not: array order in a client-controlled document carries no
meaning, and re-prompting on it would be noise. Fields are length-prefixed so
no two distinct tuples can produce the same byte stream, including when a
field itself contains the delimiter.

Rejected: hashing the whole fetched document. Strictly safer, but it re-prompts
on cosmetic edits (`logo_uri`, contacts), and a prompt that fires for reasons the
user cannot see trains people to click Approve without reading — which costs more
security than the extra fields buy. Also rejected: `redirect_uris` alone, the
issue's stated minimum, which would let a client silently rename itself and
inherit an approval granted to a different name.

Note this risk is _created_ by combining consent records with CIMD. Neither
alone has it.

### Verified clients only

`resolveClientMeta` already returns `verified` — true on the CIMD path, where
identity is proven by fetching the URL that _is_ the `client_id`, and false on
the DCR path, where metadata is self-reported. Only verified clients get a
record; a DCR client always prompts.

Beyond the self-reporting argument, a DCR `client_id` is a random string held in
an in-memory, LRU-evicted registry. It does not survive a restart, so a record
keyed on one would be dead weight at best.

### Matching is exact

A record matches only when subject, `client_id`, RFC 8707 `resource` and
fingerprint all match. Keying on `resource` means an approval for one MCP
endpoint never covers another.

The in-memory map is keyed on subject + `client_id` + `resource`, and the
fingerprint lives in the value rather than the key. So a client whose metadata
changed re-prompts, and approving *replaces* the stale record instead of
accumulating a second one — a user can never hold two live approvals for the
same client, one of which they no longer remember granting.

### Records do not expire

The issue asks for "once, until revoked". Any TTL quietly reintroduces
re-prompting, which is the thing being removed. Records are small and bounded by
(subject × client × resource) in practice.

### Revocation and theft clear consent; expiry does not

`RefreshTokenStore.revokeGrantLocked` is reached from three callers, and they
must not behave alike:

| Caller                            | Clears consent | Why                                                                                                                                                              |
| --------------------------------- | -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `RevokeGrant` (RFC 7009 endpoint) | Yes            | A record outliving revocation degrades revoke into "grant one more silent re-authorization", which is worse than not revoking because it appears to have worked. |
| `Rotate`, theft branch            | Yes            | A replayed token burned the family. Silently re-granting on the next authorize is exactly wrong.                                                                 |
| `Rotate`, expiry branch           | **No**         | Consent outliving the refresh token is the entire feature.                                                                                                       |

The decision therefore cannot live inside `revokeGrantLocked`. It returns the
family's identity — read from its live token, of which a family has exactly
one — and each caller
decides. `RevokeGrant` returns what it revoked, and `RotateResult` carries the
burned family's `Data` on the theft path, which also removes the existing
limitation that "callers that need to log the affected subject must record it at
issue time".

## Design

### Components

- **`ConsentStore`** (`internal/mcp/oauth/consent_store.go`) — `Remember`,
  `Allows`, `Forget`, `Snapshot`, `Restore`. Mirrors `RefreshTokenStore`: a
  mutex, a map, and an on-change hook fired after the lock is released.
- **`consentFingerprint(meta *ClientMetadata) string`** — the hash above.
- **`stateFile`** (`internal/mcp/oauth/persist.go`) — owns the path and writes
  both stores as one file.

### One file, two stores

Two stores cannot each hold their own persist func: each would write the file
from its own snapshot and clobber the other's. `stateFile` owns the path,
snapshots both stores, and writes. Each store's hook becomes a plain `func()`
into it.

This revises what #74 landed: `SetPersistFunc(func(RefreshTokenSnapshot))`
becomes `SetOnChange(func())`. Both stores keep the existing discipline — the
hook fires only after the mutex is released, because taking a snapshot acquires
it, and only when state actually changed.

### File format v2

```json
{
  "version": 2,
  "timestamp": "...",
  "tokens":   { "<hash>": { ... } },
  "consumed": { "<hash>": "<grantId>" },
  "grants":   { "<grantId>": ["<hash>"] },
  "consent": [
    {
      "subject":     "user@example.com",
      "clientId":    "https://example.com/client.json",
      "resource":    "https://cetacean.example.com/mcp",
      "fingerprint": "<base64url sha256>",
      "grantedAt":   "..."
    }
  ]
}
```

Consent is a slice on disk — readable, and it avoids inventing an encoding for a
composite map key built from three free-form strings — and a map keyed on
subject + client + resource in memory. A v1 file loads with no records, the way
`cache/snapshot.go` already handles v1 against v2. The filename stays
`mcp-tokens.json`: renaming buys nothing and would silently drop tokens for
anyone tracking main.

### Authorization flow

In `handleAuthorizeGET`, after every existing validation and the identity check:

```
fingerprint := consentFingerprint(meta)
if verified && consent.Allows(subject, clientID, effectiveResource, fingerprint) {
    issue code, redirect
    return
}
render the consent page
```

Skipping the page means GET issues a code and redirects rather than rendering a
form. That is ordinary for an OAuth authorization endpoint, and `redirect_uri`
has already been exact-matched against the client's registered set before this
point, so a silently issued code still lands only where the client registered.
No CSRF token is involved because no form is submitted.

GET currently computes the effective resource via `ValidateResourceIndicator`
and discards it; it now keeps it, since the record is keyed on it.

The code-issuing tail of `handleAuthorizePOST` is extracted into a helper both
paths call, removing the duplication that would otherwise appear.

On approve, `handleAuthorizePOST` calls `Remember` when `verified`.

### The consent page says so

The page gains one line stating the decision will be remembered until revoked.
Turning "approve once" into "approve until revoked" without telling the person
is not consent.

## Threat model

This differs from #74 and must not inherit that issue's assessment by proximity.

The refresh-token half of the file holds SHA-256 hashes; stealing it gains an
attacker nothing, because the hashes are not usable credentials. Its risk is
confidentiality only.

A consent record is a **capability**. Someone able to _write_ the file can inject
a pre-approval and complete an authorization flow with no human in the loop.
Same `0600`, same "an attacker with host access has already won" caveat — but the
property being protected changes from confidentiality to integrity, and the
documentation should say that rather than let the reader generalize from #74.

## Testing

- Fingerprint: stable under `redirect_uris` reordering; changes on rename, and
  on an added, removed or edited URI.
- `Allows` returns false on a mismatched subject, `client_id`, `resource` or
  fingerprint.
- A GET with a matching record redirects with a code and renders no form.
- A GET re-prompts after the client's metadata changes.
- A DCR (unverified) client records nothing and always prompts.
- `RevokeGrant` clears the record; a subsequent authorize prompts.
- Theft clears the record.
- **Expiry does not clear the record.**
- A v1 file loads with no records; a v2 file round-trips through disk.

## Out of scope

- **Revoking consent from the UI.** There is no way to list or drop a record
  other than revoking the token. Worth doing; not this issue.
- **JTI denylist for immediate access-token revocation.** Unchanged by this work
  and tracked separately.
- **Multi-replica.** The file is local. Already broken for other reasons; see
  #74.
