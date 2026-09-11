# MCP asymmetric token signing — design

**Status:** designed

Derive the OAuth server's two keys from one root instead of using one secret
directly for both, and publish the key that verifies a Cetacean-minted access
token.

## Problem

The 2026-09-09 standards sweep (`docs/plans/web-standards-ledger.md`) closed
"DPoP, device grant, JWKS/asymmetric signing" as a single line — *considered with
B3; none as close to free*. Two of the three are still correctly closed: both DPoP
and the device grant are gated on client support that does not exist. The third
was bundled with them and is a different size.

**One key is used directly for two purposes.** `ServerConfig.SigningKey` is the
same raw bytes at three call sites: the JWT key (`server.go:80`), and the HMAC key
behind the consent page's CSRF token, both issuing (`server.go:619` →
`issueCSRFNonce`) and verifying (`server.go:785` → `verifyCSRFToken`). Not a
derived pair — the bytes themselves. There is no attack here; both are
HMAC-SHA256 over structurally disjoint inputs. It is the arrangement that stops
being fine the moment a third use arrives, and it is the one defect in this
document that exists independently of anything being published.

**The authorization server has nothing it could publish.** `internal/mcp/oauth/jwt.go`
mints HS256 JWTs, so the verification key *is* the minting key and cannot be
disclosed. The RFC 8414 metadata document accordingly advertises no `jwks_uri`,
and neither does the `/.well-known/openid-configuration` alias served from the
same handler (`server.go:143`), where OIDC Discovery requires one.

## Value, stated honestly

Two claims that could be made for this are false, and are not made.

**No MCP client is waiting for this.** In the MCP authorization profile the
client is an OAuth *client*: it obtains a token and presents it as a bearer
credential. Validation is the resource server's job, and here the resource server
is this same process. `jwks_uri` is inert to every MCP client that exists.

**It does not make the OIDC alias conformant.** The alias would still lack
`id_token_signing_alg_values_supported`, `subject_types_supported` and
`response_modes_supported`; a scanner checking OIDC Discovery's required members
fails before and after. `jwks_uri` is not required by RFC 8414 at all, so the
honest OAuth document is already conformant without it.

What is left is real but modest:

- The raw key reuse becomes key derivation.
- "Can verify" stops implying "can mint", which is what makes publishing a
  verification key possible at all. That is the precondition for an edge verifier
  — Envoy's JWT filter, oauth2-proxy — not the delivery of one.
- The authorization server's surface is completed in the register this project
  already writes in. D1's web app manifest has no user who installs; C2 shipped
  an OpenSearch document the ledger itself notes Chromium has narrowed support
  for. A thin present-day consumer has not been disqualifying here.

It should not be sold as closing a hole.

## Scope

**In:** one root secret with two derived keys; ES256 access tokens carrying a
`kid`; a JWKS endpoint; `jwks_uri` in the AS metadata.

**Out:** DPoP, the device grant, key rotation, and bring-your-own-key.

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
 ├─ hkdf.Key(sha256, root, salt, "cetacean/mcp/csrf/v1",        32) → HMAC key
 └─ hkdf.Key(sha256, root, salt, "cetacean/mcp/jwt/es256/v1/N", 32) → P-256 scalar
```

`crypto/hkdf` (Go 1.24) and `ecdsa.ParseRawPrivateKey` (Go 1.25) make this
first-class stdlib. Nothing about config changes: `mcp.signing_key` keeps its
name and its `_FILE` variant. Restart stability and multi-replica agreement are
inherited from the one setting that already documents both, and the
no-persistence case needs no branch — an ephemeral root derives an ephemeral key
pair, exactly as it derives an ephemeral HMAC key today.

`hkdf.Key` — Extract **and** Expand — not `hkdf.Expand`. RFC 5869 §3.3 permits
skipping Extract only when the input is already uniformly random key material,
which the next decision shows is not guaranteed here. The salt is the constant
`cetacean/mcp/hkdf/v1`. Both halves are part of the wire contract: changing
either silently changes every derived key, and both produce plausible-looking
bytes either way.

### The root is not required to be key material, and that now matters more

`checkSigningKeyLength` (`config/mcp.go:339`) tests `len(key) >= 32` on a
*string*, and `main.go:750` converts that string to bytes. A 32-character
passphrase passes. It is a length floor over UTF-8, not an entropy floor.

Today, grinding a weak root offline requires first capturing a valid token to
test candidates against. After this change the public key is a deterministic
function of the root and the key set is unauthenticated by design, so the access
requirement collapses to an anonymous GET: grind candidate roots → HKDF → scalar
→ scalar multiply → compare against the published `x`/`y`. Recovering the root
also yields the CSRF key, because both derive from it.

So: at load, if the configured value decodes as hex or base64 to exactly 32
bytes, those bytes are the root; otherwise the raw bytes are, and startup logs a
warning naming `mcp.signing_key` and recommending `openssl rand -hex 32`. This
matches the contract `auth.oidc.session_key` already documents ("hex 32 bytes")
and costs existing deployments nothing beyond what this release already costs
them — every derived key changes in this release regardless, so a value that
newly decodes as hex changes meaning inside a migration that was happening
anyway.

Rejected: refusing a non-decodable value outright, which would fail startup for
a deployment that has been running fine on a passphrase.

### ES256, not EdDSA

Ed25519 is the nicer primitive and `ed25519.NewKeyFromSeed` is the cleaner
derivation, but verifier support for the RFC 8037 `EdDSA` algorithm is patchier
than for ES256 outside modern JOSE libraries. Since the point of publishing at
all is to be legible to something that is not us, the widely-supported algorithm
wins. `ParseRawPrivateKey` removes the only argument that pointed the other way,
which was that P-256 derivation is awkward: `ecdsa.GenerateKey` cannot be made
deterministic (`randutil.MaybeReadByte` exists specifically to prevent it), but
deriving the scalar directly sidesteps that entirely.

### Raw `R‖S`, never ASN.1

RFC 7518 §3.4 defines the ES256 signature as the two 32-byte integers
concatenated. `ecdsa.SignASN1` returns DER, which is the wrong encoding and
roughly 70 bytes. Signing therefore uses `ecdsa.Sign` with `FillBytes` into a
fixed 64-byte buffer — the fixed width matters, since a small `r` left unpadded
produces a signature this server's own verifier would accept and every other
implementation would reject. Verification refuses any signature that is not
exactly 64 bytes, so a DER blob cannot be presented as one.

Since Go 1.26 `ecdsa.Sign` ignores its reader and always uses a secure source, so
signatures are randomized: two tokens over identical claims differ.

### `kid` is a thumbprint, not stored state

The `kid` is the RFC 7638 JWK thumbprint of the public key, so it is a function
of the key and never needs persisting or configuring. `go-jose/v4` is already a
direct dependency (`go.mod:14`, used only in tests today) and provides both
`JSONWebKey.Thumbprint` and the JWK marshalling.

A `kid` in the header is the one thing that must be right on day one. Rotation is
out of scope, but a header without a `kid` makes rotation a format change later.

### The key set is served at `/oauth/jwks`

Not `/.well-known/jwks.json`, for two independent reasons.

`resolveExtension` (`negotiate.go:134-143`) strips a known extension suffix and
**mutates `r.URL.Path`** before routing, and `negotiate` wraps the whole mux
(`router.go:845`). A request for `…/jwks.json` would therefore reach the mux as
`…/jwks`, match no pattern, fall through to `/`, and — because the stripped
suffix already resolved to `ContentTypeJSON`, while the fallback refuses only
`ContentTypeUnsupported` (`router.go:820`) — be answered by the SPA with 200
`text/html`. `jwks_uri` would advertise an HTML page. No route in the tree ends
in `.json` today (`/api/context.jsonld` does not, since `.jsonld` is not a
`.json` suffix), so this collision is unexercised.

Separately, `jwks.json` is not an IANA-registered well-known URI suffix, where
the four documents already served under `/.well-known/` all are. RFC 8615 §3
requires registration, and `jwks_uri` is a free-form URL in RFC 8414, so nothing
about the location is forced.

`/oauth/jwks` costs one line in `isExempt`, whose `/oauth/…` case is an explicit
path whitelist (`internal/auth/middleware.go:101-103`).

### Exactly one key in the set, and no rotation

The published set holds the one derived key. Rotation happens the way it happens
today: the operator changes `mcp.signing_key` and restarts, which invalidates
live access tokens and costs each client one refresh.

Two consequences to state rather than design around. With `mcp.signing_key`
unset, `main.go:750-757` generates a random root per start, so the published set and
its `kid` change on **every restart** — which means the precondition for an edge
verifier is asymmetric signing *plus a configured root*, and only the first half
is delivered here. And a one-key set served with `max-age=3600` gives a verifier
that honours the cache and does not refetch on an unknown `kid` up to an hour of
failure after a root change. Both are zero-impact while nothing external
verifies, and both are the first things to revisit if something does.

### The OIDC alias stays

Deleting it is the only change that would make the `openid-configuration` path
*true*, and it is not free: some OAuth clients probe that path before RFC 8414,
so removing it is a compatibility risk rather than a one-line cleanup. It keeps
inheriting `jwks_uri` along with everything else in the document. The value
section says plainly that this does not make it conformant.

## Design

### Components

| File | Change |
|---|---|
| `internal/mcp/oauth/keys.go` | **new** — derivation |
| `internal/mcp/oauth/jwks.go` | **new** — the JWKS handler |
| `internal/mcp/oauth/jwt.go` | `TokenIssuer` signs and verifies ES256 |
| `internal/mcp/oauth/server.go` | derive in `NewServer`; route; `jwks_uri` in `asMetadata` |
| `internal/auth/middleware.go` | one `isExempt` case for `/oauth/jwks` |
| `internal/config/mcp.go` | decode the root; warn when it is not key material |

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
the raw root would silently defeat the separation.

A derived scalar lands outside `[1, n-1]` with probability about 2⁻³²;
`ParseRawPrivateKey` reports that, and derivation retries with an incremented
counter in the `info` string so the outcome stays deterministic. The retry is a
separate function over an injectable derivation step, because it cannot be
reached with a real root.

### Signing

`TokenIssuer` swaps `SigningKey []byte` for the key and the `kid`. The header
stops being a package-level constant (`jwt.go:41`) and is precomputed per issuer:
`{"alg":"ES256","typ":"at+jwt","kid":"…"}`. The `typ` check B3 introduced is
untouched; the `alg` check becomes `ES256` under the same defence-in-depth
reasoning already stated there. There is no `kid` check — with one key, the
signature check subsumes it.

### Publication

`GET {base}/oauth/jwks` serves a one-key set with `use: "sig"`, `alg: "ES256"`
and the `kid`, as `application/jwk-set+json` (RFC 7517), with
`Cache-Control: max-age=3600` to match its sibling documents. Since E7,
`negotiate` "resolves and records; it does not refuse" (`negotiate.go:69`), so
a handler that writes its own `Content-Type` needs nothing from that machinery —
the same way `HandleMetadata` already works.

`asMetadata` gains `jwks_uri`, built from `issuerID()` like every other URL so the
base path travels with it. The API catalog needs no change: the discovery chain is
catalog → PRM → `authorization_servers` → AS metadata → `jwks_uri`.

### What breaks

Access tokens live at the moment of upgrade are refused for algorithm mismatch,
costing one refresh — the trade `jwt.go` already argues for B3, unchanged, since
access tokens are never persisted and refresh tokens are opaque. A consent
approval *in flight* across the upgrade fails and costs one re-approval, because
the CSRF key is now derived rather than raw. Nothing persisted depends on either
key: `persist.go` stores refresh tokens under a plain SHA-256 of the token and
consent under `(subject, client, resource)`.

One case is worse than a single refresh: a **rolling upgrade of multiple replicas
sharing a root**. Old and new replicas behind one address reject each other's
access tokens on `alg`, so a client can refresh in a loop; and `RefreshTokenStore.Rotate`
(`store.go:261`) burns the whole grant family when a consumed token is
re-presented, which turns a concurrent double-refresh into `Theft: true` and a
full re-authorization including re-consent. Cetacean is normally a single
replica, but the upgrade note must say: stop all replicas across this upgrade.

No configuration changes beyond the root's decoding. `CETACEAN_MCP_SIGNING_KEY`'s
documented meaning becomes "the root from which the token and CSRF keys are
derived".

## Testing

**The discovery test drives the assembled router, not a bare mux.** The existing
`TestDiscoveryIssuerIncludesBasePath` (`discovery_basepath_test.go:40-41`) builds
an `http.NewServeMux()` and calls `RegisterRoutes` on it directly — no
`negotiate`, no `basePathMiddleware`, no SPA fallback — and production wires
neither the same way (`router.go:812` passes basePath `""` and strips upstream).
Extending it would assert nothing about what is served. The new test builds
`NewRouter` with `OAuthRoutes` wired, follows `jwks_uri` out of the AS metadata
over the real middleware stack, and asserts the response's `Content-Type` and
that it parses as a key set. That is the test that fails on the `.json` trap
above, and it is the one this item cannot ship without.

**go-jose is an independent verifier for the signature, and not for the JWK.**
Minting with our code and verifying with go-jose from the published document is
a real cross-check of `R‖S` padding — our code packs, go-jose parses. It is *not*
a check of the coordinate encoding, because go-jose both marshals and parses
`x`/`y`: a systematic padding bug there round-trips green while every other
implementation rejects the key. So the published document is also asserted
literally — `x` and `y` each 43 characters of unpadded base64url decoding to
exactly 32 bytes — plus one golden `kid` vector for a fixed root.

Around those, in the habit #213 established, every rule verified by breaking it:

- Swapping the two `info` strings must fail a test.
- A DER signature over the same signing input must be refused, as must any
  signature that is not exactly 64 bytes.
- The scalar retry, driven with a stubbed derivation yielding an out-of-range
  value first, recovers and stays deterministic.
- Derivation is deterministic: one root yields one `kid` across calls; two roots
  yield two. A hex-encoded root and its decoded bytes yield the same `kid`.
- The header `kid` equals the `kid` in the published set.
- The consent path no longer accepts a CSRF token signed with the raw root.

## Rejected alternatives

**Stop after the derivation: HKDF two HS256 keys and publish nothing.** This
achieves the one defect that exists independently of publication, at a fraction
of the surface — no endpoint, no algorithm change, no `kid`, no `R‖S`, no scalar
retry. It is rejected because it forecloses rather than defers: a symmetric key
cannot be published, so "derive now, publish later" means redoing the token path
later anyway, and the `info` strings would be renamed when it happened. The
derivation is also the smaller half of the work, and it is a prerequisite of the
other half rather than an alternative to it.

**A second configured key with a PEM file.** The shape this started as; see the
first decision. Its one real loss is bring-your-own-key, which has no consumer
while nothing external verifies.

**Keeping HS256 alongside ES256, selected by config, or verifying both during a
transition window.** Two code paths and an `alg` list in the metadata. The cost
this would avoid is not "one refresh" — in the rolling-upgrade case above it is a
burned grant family — but that case is answered by stopping the replicas
together, which is one sentence of documentation against a permanent second code
path in the security-critical file.

## Follow-ups, not in scope

- If an edge verifier ever appears: rotation with an overlap window, a stable
  configured root as a documented requirement, and bring-your-own-key. Each is
  speculative until then, and the one-key set plus `max-age=3600` is the debt to
  pay first.
- Whether `/.well-known/openid-configuration` is completed or dropped. This
  document makes it no less honest, and no more.
- `jwks_uri` also exists in RFC 9728 for a *resource's* keys, which would only
  matter if signed responses were ever offered.
