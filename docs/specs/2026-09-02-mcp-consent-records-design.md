# MCP consent records

**Issue:** [#160](https://github.com/Radiergummi/cetacean/issues/160)
**Depends on:** [#74](https://github.com/Radiergummi/cetacean/issues/74), merged as `0f71654e`
**Status:** implemented

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
  both stores as one file, under its own mutex.

### One file, two stores

Two stores cannot each hold their own persist func: each would write the file
from its own snapshot and clobber the other's. `stateFile` owns the path,
snapshots both stores, and writes. Each store's hook becomes a plain `func()`
into it.

Being the single writer takes a mutex, held across the snapshot and the rename
that publishes it. Two concurrent mutations — reachable now that two independent
stores point here — would otherwise snapshot at different moments and rename in
either order, publishing a view taken before the other's mutation and silently
dropping a token or an approval still live in memory. The temp file also gets a
unique name instead of a fixed `path + ".tmp"`: within the process the mutex
settles it, but two processes sharing a data directory would truncate the same
temp file and interleave bytes into it, and the rename would publish something
that does not parse. A lost update is survivable; unparseable bytes cost every
client a re-authorization.

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
render the consent page, carrying fingerprint
```

Skipping the page means GET issues a code and redirects rather than rendering a
form. That is ordinary for an OAuth authorization endpoint, and `redirect_uri`
has already been exact-matched against the client's registered set before this
point, so a silently issued code still lands only where the client registered.
No CSRF token is involved because no form is submitted.

GET currently computes the effective resource via `ValidateResourceIndicator`
and discards it; it now keeps it, since the record is keyed on it.

The code-issuing tail of `handleAuthorizePOST` is extracted into a helper both
paths call, removing the duplication that would otherwise appear. So is the
consent-page render, which GET and POST both reach.

On approve, `handleAuthorizePOST` calls `Remember` when `verified`.

### The fingerprint travels from the GET, not resolved again on the POST

The claim above — an approval is bound to a fingerprint of what the user was
shown — is not delivered by fingerprinting on the POST. `resolveClientMeta`
runs twice: once on GET to render the page, once on POST to act on it. The CIMD
fetcher's 1h cache usually hides the gap, but a consent tab open longer than
that, a restart or an eviction makes the POST re-fetch, and it would then record
a fingerprint of a document the user never saw. Every later authorize would
silently issue codes against a redirect URI set nobody approved — precisely the
attack the fingerprint exists to prevent.

So the fingerprint is computed once, on GET, and carried into the POST as a
hidden form field, folded into the existing CSRF HMAC. That HMAC already covers
`nonce` and `state` and is already verified on every POST, so binding the
fingerprint to it adds no new trust machinery: a token verifies only for the
fingerprint it was issued with, which makes the submitted value unforgeable
without making it secret.

The HMAC's fields are length-prefixed rather than `|`-joined, the same
discipline `consentFingerprint` and `consentKey` already apply. `state` is
client-chosen and may contain any byte, so a separator alone would let one
field's content spell another's.

On POST, after CSRF verification passes, the metadata is re-resolved as before
and compared against the submitted fingerprint. If they differ the client
changed while the user was deciding: **re-prompt** — render the page again from
the fresh metadata with a fresh CSRF nonce — rather than issue a code or record
anything. The record is written with the submitted fingerprint, now proven
current.

The fingerprint is computed and carried for unverified (DCR) clients too. It is
never remembered for them, but a uniform path costs one hash and no branch, and
the comparison still re-prompts when the name or redirect URI on screen went
stale — an LRU eviction and re-registration rather than a document edit, but the
user is equally owed a page describing the client about to receive the code.

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
- Approving through the consent page records the approval, and the next GET
  skips the page — driven through the real handlers, not by seeding the store.
- A client whose metadata changes between the GET and the POST re-prompts and
  records nothing; the re-prompt is itself approvable.
- A tampered fingerprint fails the CSRF check outright.
- A DCR (unverified) client records nothing and always prompts.
- The consent page states that a verified client's approval is remembered, and
  does not state it for an unverified one.
- `RevokeGrant` clears the record; a subsequent authorize prompts.
- Theft clears the record, driven through the token endpoint.
- **Expiry does not clear the record.**
- A v1 file — with a populated token map, so a nested rather than flattened
  `RefreshTokenSnapshot` embedding would be caught — loads with no records and
  keeps its refresh tokens; a v2 file round-trips through disk.

## Out of scope

- **Revoking consent from the UI.** There is no way to list or drop a record
  other than revoking the token. Worth doing; not this issue.
- **JTI denylist for immediate access-token revocation.** Unchanged by this work
  and tracked separately.
- **Multi-replica.** The file is local. Already broken for other reasons; see
  #74.
