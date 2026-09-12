# HTTP semantics: preconditions, async preferences, and compression — design

## Problem

Cetacean implements an unusually wide slice of the web platform — JSON-LD, RFC 9457,
RFC 8288 pagination, RFC 8631 discovery, RFC 6902/7396 patches, conditional reads,
Atom with RFC 5005 paging, JSON Feed, the OAuth 2.1 profile — and the 2026-09-09
standards sweep (`docs/plans/web-standards-ledger.md`) found three holes in the
HTTP layer itself. This design covers those three plus the composition prerequisite
they share and one defect the sweep turned up.

**Writes cannot express a precondition.** `internal/api/etag.go` computes a strong
ETag on every JSON response and honours `If-None-Match` for reads, but grep returns
zero hits for `If-Match`, `If-Unmodified-Since` and `StatusPreconditionFailed`
across the tree. Lost-update prevention exists only as a side effect of Docker
rejecting a stale `swarm.Version`, surfaced as a 409 by `writeDockerError`
(`internal/api/write_helpers.go:114`). A client cannot *ask* for it, and the
meaning of that 409 depends on backend behaviour rather than on a contract the API
states. Two concurrent merge-patches against `PATCH /services/{id}/env` silently
drop variables.

**REST and MCP disagree about when a mutation is done.** `PUT /services/{id}/scale`
returns as soon as Docker accepts the spec. MCP treats the same mutation as a task
and waits for convergence (`internal/mcp/tasks.go:92`), over a rule deliberately
shared at `cluster.ServiceConverged` (`internal/cluster/state.go:55`) so the two
transports cannot drift on what "settled" means. They drift anyway, because only
one of them waits. `internal/api/prefer.go` already implements RFC 7240
`return=minimal`, and `prefer_test.go:31-46` already feeds `respond-async` through
the parser — the header shape is understood, the preference is simply not offered.

**Nothing is compressed.** Zero hits for `gzip`, `zstd` or `Content-Encoding` in
`internal/`; Go's `net/http` decompresses as a client and never compresses as a
server. Measured on the current build, the SPA's first-load payload is 1048 KB
across seven chunks plus CSS, which gzip takes to 314 KB and zstd to 290 KB — a
72% reduction on the critical path, currently left on the floor. JSON-LD
collections are the same shape of win at smaller scale.

**And the middleware chain hides ordering bugs.** The global stack is built by
reassignment at `internal/api/router.go:674-682`, where the last line is the
outermost wrapper, so it reads in reverse execution order. That is how E1 hid:
`cors` (line 680) runs *before* `negotiate` (line 678), so `negotiate`'s
`w.Header().Set("Vary", "Accept")` clobbers the `Add("Vary", "Origin")` that
`cors.go:94` wrote. A cross-origin response therefore carries a reflected
`Access-Control-Allow-Origin` while announcing `Vary: Accept` only, and a shared
cache may serve it to a different origin. `cors_test.go:42` passes because it
exercises the middleware in isolation, where nothing downstream overwrites it.

## Scope

In: **A0** middleware composition, **A1** `If-Match`, **A2** `Prefer: wait` /
`respond-async`, **A3** compression, **E1** the `Vary` fix.

Out, and tracked separately in the ledger: AsyncAPI, CloudEvents, OpenMetrics
exemplars, and B4 (`net/http.CrossOriginProtection`), which wants A0 in place
first.

---

## A0 — Middleware composition

A `Chain` value type derived from `justinas/alice`, vendored as roughly 25 lines
in `internal/api` rather than imported. The package is small enough that the ideas
travel better than the dependency, and this project ships its SBOM as a product
feature.

**Taken from alice:**

- `Chain` as an **immutable value**, not a bare `chain(...)` function, so a base
  chain can be named once and derived per resource and tier — which is exactly how
  the 50 gated routes cluster.
- **`Append`'s exact-capacity fresh-slice copy.** This is the load-bearing part.
  Without it `base.Append(tier2)` and `base.Append(tier3)` alias one backing array
  and silently contaminate each other. The same defensive copy belongs in `New`,
  so a caller's slice cannot mutate a built chain.
- **`Extend`**, for composing chains out of chains.
- **`ThenFunc`** — required, not sugar: handlers are method values
  (`h.HandlePatchServiceEnv`, i.e. `func(http.ResponseWriter, *http.Request)`)
  while `mux.Handle` wants an `http.Handler`.

**Deliberately not taken:** `Then(nil)` falling back to `http.DefaultServeMux`.
This codebase never touches the package-global mux, and resolving a nil handler to
it converts a programming error into a routing mystery. Dropping it also removes
the only reason for `ThenFunc`'s nil branch.

**Two signature normalizations**, both mechanical:

1. `requireLevel` (`internal/api/write_middleware.go:15`) is
   `func(http.HandlerFunc) http.Handler` — inconsistent with every other
   middleware in the tree, and the reason `tier1(h.HandleScaleService)` compiles on
   a bare method value today. It becomes `func(http.Handler) http.Handler`, with
   routes terminating in `ThenFunc`.
2. `securityHeaders` (`internal/api/router.go:720`) takes
   `(next, tlsEnabled, inlineScriptHashes)` and is curried to
   `(tlsEnabled, hashes) -> Constructor`.

`cors`, `negotiate`, `recovery`, `requestID`, `realIP`, `requireReady`,
`discoveryLinks`, `requestLogger` and `requireWriteACL` already have the right
shape.

The global stack then reads in execution order, and per-route gates flatten from
`svcACL(tier2(precond(rep)(handler)))` to
`chain(svcACL, tier2, precond(rep)).ThenFunc(handler)`.

---

## A1 — Conditional writes: `If-Match` → 412

**Optional everywhere.** `If-Match` is honoured when sent and ignored when absent.
No 428, no client breakage, nothing existing changes. RFC 9110 permits a server to
require preconditions; we do not.

**Strict same-URI comparison.** `If-Match` compares against the ETag of the GET at
**the same URI**. `PATCH /services/{id}/env` compares against `GET
/services/{id}/env`; `DELETE /services/{id}` compares against `GET
/services/{id}`. This is what RFC 9110 §13.1.1 means by the current representation
of the target resource.

Enumerated from the router: **32 of the 53 mutating routes already have a GET at
their own URI.** Three of those are collection creates (`POST /configs`,
`/secrets`, `/plugins`) and are excluded — a precondition meaning "create this only
if the entire collection is unchanged" is valid HTTP and a bad affordance, since
the collection ETag turns over on any member change. That leaves **29 endpoints
with precondition support**: the service sub-resources (env, labels, resources,
placement, ports, mounts, networks, configs, secrets, log-driver, update-policy,
rollback-policy, healthcheck, mode, container-config, endpoint-mode), node labels
and role, config and secret labels, and the resource roots
`/services/{id}`, `/nodes/{id}`, `/configs/{id}`, `/secrets/{id}`,
`/networks/{id}`, `/volumes/{name}`, `/tasks/{id}`, `/stacks/{name}` and
`/plugins/{name}`.

Those resource roots are worth calling out, because they are where this approach
earns its keep. Networks, volumes, tasks, stacks and plugins carry **no Docker
version at all**, so the rejected version-derived scheme could have offered them
nothing. Comparing against our own representation gives `DELETE /networks/{id}` a
working precondition regardless.

> **Superseded.** The ledger originally proposed deriving a precondition tag from
> `swarm.Version` and documenting that it is not the read ETag. That is rejected:
> such a tag is not the entity-tag of any representation a client can GET, so it
> would be a bespoke precondition wearing `If-Match`'s clothes. Its stated
> advantage — immunity to unrelated cross-reference churn — turns out to matter
> only for root-detail endpoints, and not at all for the read-modify-write
> sub-resources where lost updates actually bite.

**The refactor this requires.** To compare against "the ETag that GET would emit",
the check must build the identical representation, and each projection is currently
welded inside its GET handler. So each paired endpoint splits its projection into a
`func(*http.Request) (any, bool)` representation builder; the GET handler becomes a
thin wrapper over it, and the precondition middleware calls the same function. This
is sound because `DetailResponse.MarshalJSON` already guarantees deterministic key
ordering *for stable ETags* — the property A1 needs was built for another reason
and is already there. 29 pairs; this is the bulk of A1's cost and the riskiest
change in this design.

**Placement.** A `precond(rep)` middleware composed in the A0 chain alongside
`svcACL` and `tier*`, so the route table keeps reading as a legible policy table.
Rejected alternative: putting the check inside the write helpers. There is no single
choke point — `writeMutation` has 12 callers but `writeMutationResponse` has 22
direct ones — so it would need adding in two places and would sit further from the
route table where the policy belongs.

**Strong comparison is a new function.** RFC 9110 §13.1.1 requires **strong**
comparison for `If-Match`, while §13.1.2 requires **weak** for `If-None-Match`. The
existing `etagMatch` strips the `W/` prefix and would happily match a weak
validator — correct for its current caller, a silent spec violation for this one.
Add `etagMatchStrong`, which rejects any `W/`-prefixed candidate outright.

**Error handling.** New `API013 Precondition Failed` → 412, added to `errorRegistry`
(`internal/api/errors.go:55`). `If-Match: *` against a resource with no current
representation is **412, not 404**, per RFC 9110 §13.2.2. No new existence
disclosure: everything reaching this middleware has already passed
`requireWriteACL`.

**Coverage.** The remaining **20 routes have no GET at their own URI** and
therefore advertise no precondition support, documented per endpoint in
`api/openapi.yaml`. Since `If-Match` is optional, "this endpoint has no
precondition" is a coherent contract rather than a hole. They are:
`/services/{id}/scale`, `/image`, `/rollback`, `/restart`;
`/nodes/{id}/availability`; the five `/swarm/*` section PATCHes (`raft`,
`orchestration`, `encryption`, `dispatcher`, `ca`) and the four swarm actions
(`unlock`, `rotate-token`, `rotate-unlock-key`, `force-rotate-ca`); the five
plugin routes (`privileges`, `enable`, `disable`, `settings`, `upgrade`); and
`POST /-/resync`.

The swarm section PATCHes and `/plugins/{name}/settings` are the uncomfortable
ones — they are read-modify-write config edits, exactly where preconditions matter,
and they miss out purely because they were never given the paired GET that the
service sub-resources have. Adding those GETs would extend coverage for free and is
listed as a follow-up rather than smuggled into this design.

---

## A2 — `Prefer: wait` and `respond-async`

**The wait loop moves into `internal/cluster`.** Today the predicate
(`ServiceConverged`) is shared and the polling around it is MCP's alone. That split
is wrong by the package's own doctrine — *a rule both transports must apply belongs
here* — because the polling carries a rule that is not obvious and not in the
predicate:

> The cache is filled asynchronously by the event watcher, so at the moment a write
> returns it still holds the spec and the tasks from *before* it: a scale from two
> to five asks five running against a desired two and settles instantly, and a
> scale from five to two asks two against a desired five and does the same. Neither
> is a convergence, and no predicate over those numbers could tell.
> — `internal/mcp/tasks.go`, on `minVersion`

A REST `Prefer: wait` written independently would poll `ServiceConverged`, look
correct, pass tests against a fake cache, and return immediately with a false
answer on every real scale in both directions.

New `cluster.AwaitService(ctx, cache, svcID, minVersion, poll, timeout) (progress
string, err error)`. REST passes the same values MCP uses today — a 500ms poll
interval and a 5 minute ceiling, `convergencePollInterval` and
`convergenceTimeout` from `internal/mcp/tasks.go`, which move into
`internal/cluster` beside the function. A `wait` above the ceiling is clamped to
it, which is where the `wait=300` in the table below comes from. `internal/mcp/tasks.go`'s `awaitServiceConvergenceFor` becomes
a thin wrapper over it.

**One difference stays in the wrappers, and is the reason this is a wrapper rather
than a move.** MCP detaches with `context.WithoutCancel` because mcp-go runs the
task on a goroutine holding an already-cancelled HTTP request context. REST is the
opposite: the request context is live, and a client that hangs up *should* cancel
the wait. `cluster.AwaitService` honours whatever context it is handed and never
detaches on its own.

**`minVersion` is free in REST.** `writeServiceMutation`'s `fn()` returns Docker's
post-mutation service, and `docker.Client` ends every write with a fresh
`InspectService`, so `svc.Version.Index` is available at the callsite — the same
source MCP uses.

**Wire behaviour.** `internal/api/prefer.go` gains `preferWait(r) (time.Duration,
bool)` and `preferRespondAsync(r) bool` beside the existing `preferMinimal`.

| Request | Response |
|---|---|
| no `Prefer` | unchanged, byte for byte |
| `wait=30`, converges | 200 + detail response + `Preference-Applied: wait=30` |
| `wait=30`, times out | 202 + last progress line, **no** `Preference-Applied` |
| `wait=600` | clamped to the server ceiling; `Preference-Applied: wait=300` reports what was actually applied, per RFC 7240 §2 |
| `respond-async` | 202 + `Location: /services/{id}` + `Preference-Applied: respond-async` |
| `respond-async, wait=10` | wait up to 10s, then 202 + `Location` |
| `return=minimal, wait=30` | wait, then 204 |

`Location` points at the resource itself rather than a new task resource. RFC 7240
§4.1 asks only that it identify somewhere the client can obtain status, and
`swarm.Service.UpdateStatus` reports convergence at that URI with a per-resource
SSE stream beside it. This avoids inventing a REST task resource with its own
lifecycle, TTL and ACL story. A secondary benefit: 202 is a more honest status than
today's 200, which claims completion the moment Docker accepts the spec.

**Scope: services only.** Node drain convergence ("no running tasks left on this
node") is a different rule that does not exist in `internal/cluster`, and inventing
it here would be scope creep dressed as symmetry. `PUT /nodes/{id}/availability`
gets no `wait`.

**Hooked at `writeServiceMutation`**, giving all 7 of its callers `wait` support at
once — the four MCP treats as tasks plus the spec edits that also trigger task
replacement. The 22 handlers calling `writeMutationResponse` directly are follow-up,
not part of this design.

This makes REST's `wait` coverage broader than MCP's task support, which stops at
four tools. That asymmetry is intentional and must be noted in `docs/mcp.md`, or it
reads as an oversight later.

---

## A3 — Compression

`github.com/klauspost/compress` v1.19.1 is **already in the module graph**,
indirect via `prometheus/client_golang`'s promhttp. Promoting it to a direct
dependency adds zero new modules to the SBOM.

### Half A — dynamic responses

**Negotiate early, compress in the ETag helpers.** `resolveEncoding(r)` parses
`Accept-Encoding` per RFC 9110 §12.5.3 — q-values, `identity;q=0`, `*` — and stores
an `Encoding` in the request context beside the existing `ContentType`, mirroring
`internal/api/negotiate.go` exactly.

**No `ResponseWriter` wrapping anywhere.** `writeCachedJSON` and its siblings
already marshal to a complete `[]byte` before writing, so compression here is a
pure `[]byte → []byte` transform. This is not merely convenient: it is why
`sse/broadcaster.go:143`, `log_handlers.go:205` and `metricsstream.go:61` keep
receiving the real `ResponseWriter` and their `w.(http.Flusher)` type assertions
keep working. A wrapping middleware would have had to solve that problem; this
design never creates it. (`API005` exists for precisely that failure, so a
regression would at least surface as a clean 500 rather than a silent break.)

**Encoder reuse.** `zstd.Encoder.EncodeAll` is documented safe for concurrent use,
so one package-level encoder serves every request with no pool. `gzip` gets a
`sync.Pool` of writers, having no equivalent.

**ETag: suffix, do not fold.** The coding is appended inside the quotes —
`"abc123"` for identity, `"abc123-zstd"` for zstd. This satisfies RFC 9110 §8.8.3
(distinct representations, distinct validators) and lets `Vary: Accept-Encoding`
key caches correctly, while keeping the base validator recoverable so A1's
precondition check can strip the suffix and strong-compare the base.

> **Superseded.** The original A3 decision was to fold the coding into the ETag
> *hash input*. That breaks A1: a client that reads with `Accept-Encoding: zstd`
> holds a tag computed over the zstd variant and sends it as `If-Match` on a
> request whose own negotiation may be identity. Because the coding is mixed into
> the hash, the two tags are unrelated opaque strings and no common base is
> recoverable, so the precondition fails spuriously and there is no way to repair
> it at the comparison site.

`Vary` gains `Accept-Encoding` via **`Add`, never `Set`** — the E1 lesson, now
testable because of A0.

**Below 1024 bytes, do not compress.** Small bodies frequently grow under gzip and
always cost CPU. Skipped bodies stay identity with an unsuffixed tag, which keeps
A1 consistent for free.

**BREACH.** `HandleSearch` echoes `?q=` verbatim into the response body
(`internal/api/search_handlers.go:121`, `SearchResponse.Query`) alongside
authenticated content — the textbook BREACH shape. It opts out with one line,
`r = disableCompression(r)`, before its `writeCachedJSON` call, rather than a
parallel family of no-compress helpers.

**Coverage:** JSON, Atom, JSON Feed, the JGF/GraphML/DOT topology renderings, and
the `text/csv` output from ledger item C3 when it lands. **Not** SSE — with no
`ResponseWriter` wrapping it is structurally out of reach, which is stronger than
an exclusion list.

### Half B — SPA assets, via a Vite plugin

`internal/api/spa.go` serves the embedded tree with `http.FileServer`, which the
helpers above never touch, and this is where the payload is.

**`frontend/plugins/precompress.ts`**, a build-only plugin (`apply: "build"`,
`enforce: "post"`) hooking `generateBundle`. It uses `this.emitFile` so variants go
through Rolldown's own output pipeline — respecting `outDir`, visible to later
plugins, written by Vite rather than behind its back. Node v26.4.0 provides
`zlib.zstdCompressSync`, so no npm dependency is needed. It snapshots
`Object.entries(bundle)` before emitting, so it cannot iterate over its own output.

The bundle object is why this is a plugin and not a post-build script:

- **Compressibility is decided from the bundle, not from filenames.**
  `chunk.type === "chunk"` is definitionally JS; assets carry their real source and
  byte length. An extension allowlist is a heuristic pretending to be a rule.
- **A manifest replaces per-request probing.** The plugin emits
  `assets-manifest.json` mapping each file to its available variants and their
  sizes. `spa.go` loads it once at startup and does a map lookup instead of probing
  the embedded FS on every asset request.
- **Precomputed ETags.** The plugin has the bytes, so it hashes them and records a
  tag per variant. This lands directly on `writeRawWithPrecomputedETag`, which
  already exists in `etag.go` for exactly this case — bodies fixed at build time,
  hashed once rather than per request, including the 304s that never touch the body.
- **`isEntry` and `imports`** describe the first-load set exactly. Recorded now;
  emitting `Link: rel=modulepreload` or 103 Early Hints from it is a follow-up
  ledger item, not part of this design.

**`go:embed` trap.** `main.go:37` is `//go:embed frontend/dist/*` with no `all:`
prefix, so anything under a `.`-prefixed directory is excluded with no error at any
stage. The manifest therefore goes at the dist root as `assets-manifest.json`, not
at Vite's conventional `.vite/manifest.json`.

**Serving.** `http.FileServer` cannot do variant selection, so it is replaced by a
handler that consults the manifest, sets `Content-Type` from the **original**
extension (otherwise `ServeContent` sniffs `.zst` and gets it wrong), sets
`Content-Encoding` and `Vary`, and delegates to `http.ServeContent`. Embedded files
implement `io.Seeker`, so Range and `If-Modified-Since` keep working.

**Adjacent fix, folded in.** `spa.go` currently sets **no cache headers at all**,
while Vite emits content-hashed filenames that are immutable by construction, so
every asset is revalidated on every load. Hashed assets get
`Cache-Control: public, max-age=31536000, immutable`; `index.html` gets `no-cache`
and must not be pinned. On repeat visits this is plausibly a larger win than the
compression, and the handler is being rewritten anyway.

**Not applied to `vite.config.widgets.ts`** — widgets are served as MCP resources,
not over HTTP, and have no `Content-Encoding` path.

**Measured cost and benefit.** First-load payload 1048 KB → 314 KB gzip → 290 KB
zstd (−72%). The embedded tree grows from 3.2 MB to about 5.0 MB, so the binary
grows roughly 1.9 MB. Only js/css/html/svg/json are compressed; png/ico/woff2 are
already compressed and would only inflate the binary.

---

## E1 — `Vary: Origin` clobbered

`negotiate` uses `Add` rather than `Set` for `Vary`, or the `Vary` write is hoisted
into one place that owns it. `atom_handlers.go:77`'s `Add("Vary", "Authorization,
Cookie")` is checked in the same pass — it is correct today only because it happens
to run after `negotiate`.

---

## Testing

**The lever already exists.** `TestResponsesMatchOpenAPISpec` validates real
responses against `api/openapi.yaml`, so a new 412 or 202 that is not documented
fails the build. But `TestEveryReadEndpointMatchesSpec` is exhaustive for **reads
only**, and A1 and A2 are entirely write-side. Add its counterpart: assert that
every route composed with `precond` documents `If-Match` and 412, **and that every
route without it documents neither**. This is the shape of
`TestTypeGrantsAgreesWithCan` — two declarations that would drift silently, held
together by a test that drives both.

**A0.** A chain of recording middlewares asserting declared execution order; and two
chains derived from one base not contaminating each other, which is the aliasing
bug the copy semantics exist to prevent.

**E1.** Through `NewRouter`, not the middleware alone — `cors_test.go:42` passes
today while the composed behaviour is wrong, so a test at the same altitude would
repeat the mistake.

**A1.** Per-pair table: matching `If-Match` succeeds, stale → 412, `*` on a missing
resource → 412, absent header → unchanged. Plus two that catch specific mistakes: a
`W/"..."` `If-Match` must be **rejected**, catching accidental reuse of the weak
`etagMatch`; and a **round-trip generated per pair** — GET the sub-resource, feed
its ETag back as `If-Match` — so all 29 representation extractions are proven
rather than spot-checked. Plus one cross-item test: an ETag obtained with
`Accept-Encoding: zstd` works as `If-Match` on an identity request.

**A2.** The regression test that justifies the extraction: **a 5→2 scale against a
cache still at the pre-mutation version must not report converged.** Without it the
move into `internal/cluster` is unproven. Then timeout → 202 with no
`Preference-Applied`; a clamped `wait` reporting the clamped value; client
disconnect cancelling the wait, which asserts the REST/MCP context difference is
real; and composition with `return=minimal` → 204. MCP's existing convergence tests
staying green is the proof the extraction was faithful.

**A3.** Round-trip per coding; `identity;q=0` with nothing acceptable → 406; **ETag
differs per coding while `If-None-Match` with the suffixed tag still returns 304**,
which is the "compression did not silently cost us caching" test; sub-threshold
bodies uncompressed with unsuffixed tags; `/search` never compressed; and **SSE
still streams through the assembled router**, an event readable before the stream
closes, guarding the Flusher property structurally rather than by comment.
Plugin-side, vitest drives `generateBundle` with a synthetic bundle and asserts the
emitted set, the manifest and the precomputed tags. One Go test asserts every file
named in the embedded manifest exists in the embedded FS, which also catches the
`go:embed` dotfile trap if the manifest is ever moved.

## Sequencing

1. **A0**, then **E1** on top of it. E1's fix then arrives with a test that proves
   the composition rather than only the header.
2. **A1**, with the 29 representation extractions as their own commit before the
   middleware lands.
3. **A2**, extracting `cluster.AwaitService` and rewiring MCP onto it.
4. **A3**, halves independently shippable: dynamic responses, then the Vite plugin
   and `spa.go`.

A1 precedes A3 deliberately: A3's ETag suffix is defined against the base-tag
contract A1 establishes, so the reverse order ships a suffix nothing knows how to
strip.

**The one risk worth naming.** A1's 29-pair refactor touches handlers that
currently work, and a mechanical slip there is invisible. The per-pair round-trip
test is the mitigation. Everything else in this design is additive.

## Rejected alternatives

| Considered | Rejected because |
|---|---|
| A router library (chi, echo, gin) | Go 1.22's `ServeMux` already provides the method-and-wildcard routing that was chi's main draw; a 741-line router rewrite for ergonomics 25 lines deliver |
| `justinas/alice` as a dependency | ~50 lines, and this project ships its SBOM as a feature |
| A route-registration DSL pairing GET and write | Hides which gates a route carries; the route table reading as a policy table is worth keeping |
| Version-derived precondition ETag | Not the entity-tag of any GETtable representation — a bespoke precondition wearing `If-Match`'s clothes |
| Preconditions inside the write helpers | No single choke point (12 vs 22 callers) and further from the route table where policy belongs |
| Folding the content coding into the ETag hash | Makes the base validator unrecoverable, breaking `If-Match` across differing negotiation |
| A wrapping compression middleware | Would break the three `w.(http.Flusher)` assertions; also forces buffering and taking conditional-request logic away from helpers that do it well |
| Compressing SSE | Needs per-event sync flush or the stream stops being live; excluded by construction instead |
| A REST task resource for `respond-async` | The resource itself already reports convergence via `UpdateStatus`; a task resource adds lifecycle, TTL and ACL surface for no gain |
| `wait` on node availability | Node drain convergence is a rule that does not exist yet; symmetry is not a reason to invent one |

## Follow-ups, not in scope

- The 22 handlers calling `writeMutationResponse` directly could move onto
  `writeServiceMutation` and inherit `wait`.
- `Link: rel=modulepreload` / 103 Early Hints from the manifest's `isEntry` and
  `imports` metadata.
- `vendor-charts` (209 KB) and `vendor-topology` (174 KB) are *statically* imported
  by the entry chunk, so 383 KB of the 1048 KB first load is Chart.js and React
  Flow on pages that mostly do not use them. Making those lazy would beat
  compression on the critical path. Noted, not touched.
- Paired GETs for the five `/swarm/*` sections and `/plugins/{name}/settings`,
  which would extend A1 coverage from 29 endpoints to 35 with no new machinery.
- Ledger item B4, `net/http.CrossOriginProtection`, which wants A0 first.
