# MCP asymmetric token signing — design

**Status:** designed

Publish the key that verifies a Cetacean-minted access token, and stop using one
secret directly for two purposes.

## Problem

The 2026-09-09 standards sweep (`docs/plans/web-standards-ledger.md`) closed
"DPoP, device grant, JWKS/asymmetric signing" as a single line — *considered with
B3; none as close to free*. Two of the three are still correctly closed: both DPoP
and the device grant are gated on client support that does not exist. The third
was bundled with them and is a different size.

`internal/mcp/oauth/jwt.go` mints HS256 JWTs today, and `ServerConfig.SigningKey`
is used three times: as the JWT key (`server.go:80`), and as the HMAC key behind
the consent page's CSRF token, both issuing (`server.go:619` → `issueCSRFNonce`)
and verifying (`server.go:785` → `verifyCSRFToken`). The RFC 8414 metadata
document advertises no `jwks_uri`.

Two consequences.

**Nothing outside the process can verify a token this server minted, and the
metadata does not say how it might.** The authorization server and the resource
server are the same process, so HS256 is sound rather than broken. But
`/.well-known/openid-configuration` is served as an alias for that same document
(`server.go:143`), and `jwks_uri` is *required* by OIDC Discovery — so the first
document a scanner fetches is incomplete.

**One key is used directly for two purposes.** Not a derived pair: the same raw
bytes are the HMAC key for access tokens and for CSRF tokens. There is no attack
here — both are HMAC-SHA256 over structurally disjoint inputs — but it is the
arrangement that stops being fine the moment a third use arrives.

## Value, stated honestly

No verifier exists that this unblocks, and none is planned. There is no proxy
pre-validating tokens at the edge; a second Cetacean replica would need the same
shared secret either way, so asymmetric signing buys nothing there. What it does
buy:

- The RFC 8414 document — and the OIDC alias that inherits from it — gains the
  member a client or a scanner looks for.
- "Can verify" stops implying "can mint", which is what makes publishing a
  verification key possible at all. That is the precondition for an edge
  verifier, not the delivery of one.
- Key reuse becomes key derivation.

A posture and conformance item. It should not be sold as closing a hole.

## Scope

**In:** one root secret with two derived keys; ES256 access tokens carrying a
`kid`; a JWKS endpoint; `jwks_uri` in the AS metadata.

**Out:** DPoP, the device grant, key rotation, bring-your-own-key, and either
completing or dropping the OIDC alias.

## Decisions

### One root, two derived keys — not a second configured secret

The obvious shape is a second setting holding an EC private key, auto-generated
into the data directory and overridable by a configured PEM. It was designed that
way first and is rejected: it adds a config name (and `signing_key_file` is
already taken by the HMAC key's `_FILE` variant), a file with permission
requirements, a persistence branch, and a second thing an operator must carry
between replicas.

Instead both keys derive from the existing root:

```
root  =  mcp.signing_key, or the ephemeral random key main.go generates
 ├─ HKDF-SHA256  info="cetacean/mcp/csrf/v1"        → 32-byte HMAC key
 └─ HKDF-SHA256  info="cetacean/mcp/jwt/es256/v1/N" → 32-byte scalar → P-256 key
```

`crypto/hkdf` (Go 1.24) and `ecdsa.ParseRawPrivateKey` (Go 1.25) make this
first-class stdlib. Nothing about config changes: `mcp.signing_key` keeps its
name, its `_FILE` variant and its 32-byte floor (`checkSigningKeyLength`,
`config/mcp.go:339`), which becomes the root's entropy floor. Restart stability
and multi-replica agreement are inherited from the one setting that already
documents both, and the no-persistence case needs no branch — an ephemeral root
simply derives an ephemeral key pair, exactly as it derives an ephemeral HMAC key
today.

This also resolves the key reuse above: HKDF with distinct `info` strings is the
standard construction for deriving independent keys from one secret, and it is an
improvement over the current arrangement whether or not anything is published.

### ES256, not EdDSA

Ed25519 is the nicer primitive and `ed25519.NewKeyFromSeed` is the cleaner
derivation, but verifier support for the RFC 8037 `EdDSA` algorithm is patchier
than for ES256 outside modern JOSE libraries. Since the entire point of the item
is to be legible to third-party tooling, the widely-supported algorithm wins.
`ParseRawPrivateKey` removes the only argument that pointed the other way, which
was that P-256 derivation is awkward: `ecdsa.GenerateKey` cannot be made
deterministic (`randutil.MaybeReadByte` exists specifically to prevent it), but
deriving the scalar directly sidesteps that entirely.

### Raw `R‖S`, never ASN.1

RFC 7518 §3.4 defines the ES256 signature as the two 32-byte integers
concatenated. `ecdsa.SignASN1` returns DER, which is the wrong encoding and
roughly 70 bytes. Signing therefore uses `ecdsa.Sign` with `FillBytes` into a
fixed 64-byte buffer — the fixed width matters, since a small `r` left unpadded
produces a signature that this server's own verifier would accept and every other
implementation would reject. Verification refuses any signature that is not
exactly 64 bytes, so a DER blob cannot be presented as one.

Note that since Go 1.26 `ecdsa.Sign` ignores its reader and always uses a secure
source, so signatures are randomized: two tokens over identical claims differ.

### `kid` is a thumbprint, not stored state

The `kid` is the RFC 7638 JWK thumbprint of the public key, so it is a function
of the key and never needs persisting or configuring. `go-jose/v4` is already a
direct dependency (`go.mod:14`, used only in tests today) and provides both
`JSONWebKey.Thumbprint` and the JWK marshalling, which avoids hand-rolling
canonical JSON and fixed-width coordinate encoding — the same padding trap as the
signature, twice over.

A `kid` in the header is the one thing that must be right on day one. Rotation is
out of scope, but a header without a `kid` makes rotation a format change later.

### Exactly one key in the set, and no rotation

The published set holds the one derived key. Rotation happens the way it happens
today: the operator changes `mcp.signing_key` and restarts, which invalidates
live access tokens and costs each client one refresh. Scheduled rotation with an
overlap window is what a JWKS is *for*, but with no external verifier it would be
unobserved machinery.

### The OIDC alias stays as it is

`jwks_uri` closes part of the alias's non-conformance. It remains a non-conformant
OIDC discovery document — no `id_token_signing_alg_values_supported`,
`subject_types_supported` or `response_modes_supported` — and completing it would
mean stating things about an `id_token` flow this server does not implement.
Recorded in the ledger as a finding rather than fixed here.

## Design

### Components

| File | Change |
|---|---|
| `internal/mcp/oauth/keys.go` | **new** — derivation |
| `internal/mcp/oauth/jwks.go` | **new** — the JWKS handler |
| `internal/mcp/oauth/jwt.go` | `TokenIssuer` signs and verifies ES256 |
| `internal/mcp/oauth/server.go` | derive in `NewServer`; route; `jwks_uri` in `asMetadata` |
| `internal/config/mcp.go` | doc comment on `SigningKey` only |

Plus `docs/mcp.md`, the environment table in `CLAUDE.md`, and `CHANGELOG.md`.

### Derivation

```go
type keyMaterial struct {
    csrf   []byte            // HMAC-SHA256 key for consent CSRF tokens
    signer *ecdsa.PrivateKey // ES256 access token key
    kid    string            // RFC 7638 thumbprint of the public JWK
}

func deriveKeys(root []byte) (*keyMaterial, error)
```

`NewServer` calls it once and holds the result on `Server`. `TokenIssuer` takes
the signer and the `kid`; the two consent call sites that read `s.cfg.SigningKey`
today (`server.go:619`, `server.go:785`) read `csrf` instead. Leaving either on
the raw root would silently defeat the separation. A derived scalar lands outside `[1, n-1]` with probability about
2⁻³²; `ParseRawPrivateKey` reports that, and derivation retries with an
incremented counter in the `info` string so the outcome stays deterministic. The
retry is a separate function over an injectable derivation step, because it
cannot be reached with a real root.

### Signing

`TokenIssuer` swaps `SigningKey []byte` for the key and the `kid`. The header
stops being a package-level constant (`jwt.go:41`) and is precomputed per issuer:
`{"alg":"ES256","typ":"at+jwt","kid":"…"}`. The `typ` check B3 introduced is
untouched; the `alg` check becomes `ES256` under the same defence-in-depth
reasoning already stated there. There is no `kid` check — with one key, the
signature check subsumes it.

### Publication

`GET {base}/.well-known/jwks.json` serves a one-key set with `use: "sig"`,
`alg: "ES256"` and the `kid`, as `application/jwk-set+json` (RFC 7517 §8.5.1),
with `Cache-Control: max-age=3600` to match its two sibling documents.
`/.well-known/` is already exempt from auth (`internal/auth/middleware.go:98`),
so nothing is needed there.

`asMetadata` gains `jwks_uri`, built from `issuerID()` like every other URL so the
base path travels with it. The API catalog needs no change: the discovery chain is
catalog → PRM → `authorization_servers` → AS metadata → `jwks_uri`.

One thing to confirm during implementation rather than assume: C1 found two media
types the API served but could not be asked for, and E7 made each endpoint refuse
its own set. `application/jwk-set+json` must be checked against that machinery,
not merely set as a header.

### What breaks

Access tokens live at the moment of upgrade are refused for algorithm mismatch,
costing one refresh — the trade `jwt.go` already argues for B3, unchanged, since
access tokens are never persisted and refresh tokens are opaque. A consent
approval *in flight* across the upgrade fails and costs one re-approval, because
the CSRF key is now derived rather than raw. Nothing persisted depends on either
key: refresh tokens are stored by hash, consent records by client and user.

No configuration changes. `CETACEAN_MCP_SIGNING_KEY`'s documented meaning becomes
"the root from which the token and CSRF keys are derived".

## Testing

The load-bearing test is a cross-check, not a round-trip: **mint with our code,
verify with `go-jose` using only the published JWKS document.** Our verifier
agreeing with our signer proves nothing about `R‖S` padding; an independent
implementation disagreeing is the only thing that catches a short `r`.
`internal/auth/oidc_callback_test.go` already drives go-jose this way.

Around it, in the habit #213 established — every rule verified by breaking it:

- Swapping the two `info` strings must fail a test.
- A DER signature over the same signing input must be refused, as must any
  signature that is not exactly 64 bytes.
- The scalar retry, driven with a stubbed derivation yielding an out-of-range
  value first, recovers and stays deterministic.
- Derivation is deterministic: one root yields one `kid` across calls; two roots
  yield two.
- The header `kid` equals the `kid` in the published set.
- `jwks_uri` carries the base path — extending `TestDiscoveryIssuerIncludesBasePath`.
- The AS metadata walk asserts the *shape* at `jwks_uri`, not merely a non-404:
  the SPA fallback answers 200 for anything unrouted, which is what nearly let
  C1's catalog walk pass under a renamed endpoint.

## Rejected alternatives

**A second configured key with a PEM file.** The shape this started as; see the
first decision. Its one real loss is bring-your-own-key, which has no consumer
while nothing external verifies, and which can arrive later as an override
without disturbing the derivation.

**Keeping HS256 alongside ES256, selected by config.** Two code paths and an
`alg` list in the metadata, to serve a migration nobody needs: the upgrade costs
one refresh, and B3 already set that precedent in this exact file.

**Dual-algorithm verification during a transition window.** Same reasoning. The
window would exist to save a single refresh round-trip.

## Follow-ups, not in scope

- An edge verifier (Envoy's JWT filter, oauth2-proxy) becomes possible; if one
  ever appears, rotation and bring-your-own-key become real rather than
  speculative, and should be designed then.
- The OIDC alias is either completed or dropped.
- `jwks_uri` also exists in RFC 9728 for a *resource's* keys, which would only
  matter if signed responses were ever offered.
