# AsyncAPI: a machine-readable contract for the SSE streams — design

## Problem

Cetacean serves **20 SSE streams**. OpenAPI describes four of them, and three of
those four only partially.

`api/openapi.yaml` carries exactly four `text/event-stream` content blocks:

| Line | Endpoint | What it says |
|---|---|---|
| 495 | `GET /metrics` | `schema: {type: object}` and a sentence: *"SSE stream with `initial` (full range) and `point` (single value) events"* |
| 902 | `GET /events` | `$ref: SSEEvent`, plus prose giving the frame shape |
| 2633 | `GET /services/{id}/logs` | `$ref: LogLine` |
| 4392 | `GET /tasks/{id}/logs` | `$ref: LogLine` |

Every other stream is undocumented as a stream. The eight list endpoints and
eight detail endpoints are registered with `contentNegotiatedWithSSE`
(`internal/api/router.go:318`, `:327`, and fourteen more), so `GET /services`
with `Accept: text/event-stream` opens a live stream — and nothing in the
OpenAPI document mentions that the endpoint has a streaming representation at
all. **Sixteen of twenty streams are invisible to a client reading the API
description.**

What the document cannot express even where it tries is the rest of it. OpenAPI
describes a *response body*; an SSE stream is a sequence of named frames with a
cursor, and the parts that matter have no slot to go in:

**The event-name vocabulary.** `event: batch` and `event: sync` exist only
inside the `SSEEvent` schema's prose (`api/openapi.yaml:7089`). The per-type
names appear as *"(node, service, task, etc.)"* — an `etc.` standing in for an
enumeration the code knows exactly (`internal/api/sse/broadcaster.go:440-457`).

**Resumability, which differs per channel.** `Last-Event-ID` drives real replay
from a ring buffer (`broadcaster.go:243`). A cursor too old for the ring gets a
`sync` / `full_sync` event instead, asking the client to refetch. And detail
streams are *ineligible for replay entirely* — `replayType == ""` takes the
`writeSync()` branch unconditionally (`broadcaster.go:258`). So `/services` and
`/services/{id}` are not one channel with a different filter; they make
different promises about what happens when a client reconnects.

**A replayed frame has a different shape from a live one.** Replay converts
entries "to `cache.Event` (no Resource payload)" (`broadcaster.go:274`), so
`resource` — `omitempty` on the wire (`broadcaster.go:399`) — is simply absent.
A client that assumes it is present breaks only after a reconnection, which is
the worst time to find out.

**Three incompatible cursor dialects.** Resource streams write a monotonic
`HistoryID` (`broadcaster.go:471`); log tails write an RFC3339 timestamp, and
deliberately only advance it forward, because Docker interleaves tasks and a
backwards-moving id would discard lines on the next resume
(`internal/api/log_handlers.go:245-267`); the metrics stream writes no `id:` at
all. All three are `Last-Event-ID` to a client, and mean three different things.

**`query_error`.** `internal/api/metricsstream.go:79` emits it. Nothing
documents it.

This is the last item on the 2026-09-09 web-standards ledger's deferred list
that closes a hole rather than adding a surface.

## Scope

One document, one route, two discovery entries, six tests.

**In scope:** an AsyncAPI 3.0.0 document describing all 20 channels, served at
`GET /api/asyncapi`, discoverable from the `Link` header and the API catalog,
and held true by tests that drive the real router.

**Not in scope:** changing any stream's behaviour. This describes what exists.
Where the description is awkward to write, that is evidence about the design,
recorded as a follow-up rather than fixed here.

## The document

`api/asyncapi.yaml`, hand-written, `//go:embed`-ed beside `api/openapi.yaml`.

Hand-written rather than assembled in Go — the opposite of
`internal/api/apicatalog.go`, which is built from runtime facts because its
content depends on what the router mounted. An AsyncAPI document is mostly
prose, examples and JSON Schema; expressing three dialects' worth of that as Go
struct literals fights both the authoring and every off-the-shelf AsyncAPI tool.
The drift that hand-writing invites is answered by tests, which is the answer
this repo already reaches for — `TestCSVColumnsMatchTheWidget` holds a Go column
map to a TypeScript one, and `TestTypeGrantsAgreesWithCan` holds two ACL
implementations of one rule together.

### Channels

Twenty channels, one per address, each `$ref`-ing messages from a shared pool:

| Channels | Messages | `id:` | On reconnect with a cursor |
|---|---|---|---|
| 8 list streams — `/nodes`, `/services`, `/tasks`, `/stacks`, `/configs`, `/secrets`, `/networks`, `/volumes` | `<type>`, `batch`, `sync` | monotonic `HistoryID` | replay from the ring; `sync` if the cursor has aged out |
| 8 detail streams — `/nodes/{id}` … `/volumes/{name}` | `<type>`, `batch`, `sync` | monotonic `HistoryID` | always `sync` — replay is not offered |
| `/events` (legacy, `?types=`) | all eight type messages, `batch`, `sync` | monotonic `HistoryID` | replay from the ring |
| `/services/{id}/logs`, `/tasks/{id}/logs` | `logLine` (unnamed frames) | RFC3339 timestamp, forward-only | `Last-Event-ID` narrows a `logResumeTail` backlog |
| `/metrics` | `initial`, `point`, `query_error` | none | nothing to resume |

One channel per address rather than parameterising `/{type}` and `/{type}/{id}`
over an eight-value enum. The collapsed form is a smaller file and a single edit
to add a ninth type, but it costs the two things the document exists for: the
payload's `resource` becomes a union across eight schemas instead of a named one
per channel, and the drift test has to expand the enum itself — re-deriving what
the document says rather than reading it. The list/detail split is not
cosmetic either, as the reconnect column above shows.

### Messages and the three dialects

Fourteen messages in `components.messages`: eight per-type resource events
(`node`, `service`, `task`, `stack`, `config`, `secret`, `network`, `volume`),
plus `batch`, `sync`, `logLine`, `initial`, `point` and `queryError`.

Two payload facts the schemas must carry that no prose currently does:

- **A `batch` frame's payload is an array** of envelopes, not one envelope.
  `WriteBatch` writes `event: <type>` for a single event and `event: batch` with
  a JSON array for more (`broadcaster.go:469-478`).
- **A replayed event omits `resource`.** The `sync` message and the replay path
  both produce envelopes without it.

The `/metrics` messages are the first schemas anywhere for that stream:
`initial` carries a full range query result, `point` a single value,
`query_error` the shape `marshalErrorEvent` produces.

### Schemas, and why they are copies

`components.schemas.SSEEvent` and `components.schemas.LogLine` are copies of the
OpenAPI originals (`api/openapi.yaml:7086` and the `LogLine` entry), held to
them by `TestAsyncAPISchemasMatchOpenAPI`, which parses both files and fails on
any difference.

Copies rather than `$ref`s because the correct `$ref` differs between the file
and the served document. `$ref: "/api#/components/schemas/SSEEvent"` genuinely
resolves once served — `/api` returns the OpenAPI spec as JSON — but is
meaningless in the repo, where `./openapi.yaml#/...` is correct instead. One
reference with two correct spellings depending on where it is read is the same
class of trap as the `.json` suffix that `/.well-known/jwks.json` fell into
(PR #233): a path that is right in the place you wrote it and wrong in the place
it is used.

The duplication is two schemas. The per-type `resource` payloads are not
duplicated because they do not exist on either side — Docker's `swarm.Node`,
`swarm.Service` and friends were never schematised in OpenAPI, and `resource`
stays `type: object` in both documents.

## Serving and discovery

`GET /api/asyncapi`, via `HandleAsyncAPI` in `internal/api/asyncapi.go`.

**Single representation, served unconditionally**, the way `HandleContext` does
for `/api/context.jsonld` (`internal/api/context.go:39`) — no content
negotiation branch, no SPA fallback, no 406 table entry. That is E7's
conclusion about documents that have exactly one form, and this is one.

- `Content-Type: application/vnd.aai.asyncapi+json;version=3.0.0`
- `Cache-Control: public, max-age=3600`, matching `HandleAPIDoc`
- YAML→JSON once at startup via the existing `convertYAMLToJSON`
- Body written with `writeRawWithETag`, not `newStaticBody` — see below

**No renderer.** `/api` gets the Scalar playground; `/api/asyncapi` gets the
document. A browser-side AsyncAPI viewer means `@asyncapi/react-component` as a
production frontend dependency, with a `postbuild` copy-out, an SBOM entry, a
`THIRD_PARTY_LICENSES` entry, a CSP allowance and a Dependabot group — the exact
freight `CLAUDE.md` records Scalar's vendored bundle costing when nothing
watched it. Consumers have AsyncAPI Studio.

**`servers` is per-request**, which is why the body is not a `newStaticBody`.
AsyncAPI 3.0 requires `host` on a server object, so the document names the
deployment's own origin — and that must come from the validated origin
(`originOf`, `absPath`), never from raw `X-Forwarded-*`. That is ledger item
B5's rule, and B5 exists because C1 and C2 each added a consumer for `absURL`
before anyone checked what it was built from. `writeRawWithETag` is the pattern
`HandleAPICatalog` already uses for exactly this.

**`/api/asyncapi.json` works for free** — `.json` is a stripped extension
(`internal/api/negotiate.go:121`), so the suffixed path routes to the same
handler. It gets an explicit test, because this mechanism has bitten once
already from the other direction.

**Discovery** in two places:

```
Link: </api>; rel="service-desc"
Link: </api/asyncapi>; rel="service-desc";
      type="application/vnd.aai.asyncapi+json;version=3.0.0"
Link: </api/context.jsonld>; rel="describedby"
Link: </.well-known/api-catalog>; rel="api-catalog"
```

RFC 8631 does not limit `service-desc` to one link, and the `type` parameter is
what tells a client which description it is being offered. The catalog gains a
matching `service-desc` target on the REST API's context, which is what C1 built
the catalog for.

## Testing

Six claims. Each is confirmed load-bearing by making the mutation it exists to
catch and watching it fail first — #229's note records that habit as the only
reason the catalog walk was worth writing.

**1. Every channel address is a stream.** `TestAsyncAPIChannelsAnswerAsStreams`
walks the channels, substitutes path parameters from the fixture cache, requests
each with `Accept: text/event-stream`, and asserts `200` with
`Content-Type: text/event-stream`.

This one is immune to the failure that nearly defeated C1's catalog walk, where
"every published URI answers non-404" passed under a renamed endpoint because
the SPA fallback answers `200` for anything unrouted. Here a bogus address still
reaches the SPA — and the SPA answers `text/html`, so the assertion fails on the
content type. The trap is closed by construction rather than by remembering it.

**2. Every stream is a channel.** `TestEveryStreamRouteIsAChannel` walks
`mux.patterns`, probes each `GET` with `Accept: text/event-stream`, and requires
anything that streams to appear as a channel address. This is the direction that
catches a stream route added later, and it is the reason the document can be
trusted six months from now.

**3. The document is valid AsyncAPI 3.0.** The official AsyncAPI 3.0.0 JSON
Schema is vendored to `internal/api/testdata/asyncapi-3.0.0.json` and embedded
*from a `_test.go` file*, so it is never linked into the binary. Validation runs
through `santhosh-tekuri/jsonschema/v6`, already in the module graph. No JS
toolchain and no `@asyncapi/cli` step in `make check`.

**4. The copied schemas match OpenAPI.** `TestAsyncAPISchemasMatchOpenAPI`
parses both YAML files and deep-equals `SSEEvent` and `LogLine`.

**5. The declared event names are the names emitted** — one exemplar per code
path, not per channel:

| Driven | Asserts |
|---|---|
| `/services` (list) | one mutation → `event: service`; two inside the SSE batch interval → `event: batch` with an array payload; reconnect with an aged-out `Last-Event-ID` → `event: sync`; a replayed frame omits `resource` |
| `/services/{id}` (detail) | reconnect with any cursor → `event: sync`, never a replay |
| both log tails | unnamed frames, `id:` is RFC3339 and never moves backwards |
| `/metrics` | `initial` then `point`; `query_error` against a failing Prometheus stub |

The sixteen resource channels run through two functions — `streamList` and
`streamResource` — so driving all sixteen exercises the same code eight times
each, for sixteen chances to flake. What actually varies per channel is a `type`
string, and that is pinned better structurally: **the set of per-type messages
declared in the document must equal the set of `cache.EventType` constants.** A
ninth resource type fails that comparison without anyone opening a ninth stream.

**6. Captured frames validate against their declared schemas.** Every payload
claim 5 captures is validated against the channel's message schema — the
AsyncAPI counterpart of what `openapi_exhaustive_test.go` does for responses.

## Sequencing

1. The document, with channels and messages but no serving — claims 3 and 4
   pass against the file alone.
2. The route, the `Link` header and the catalog target — claim 1, plus the
   `.json` suffix test.
3. Claim 2, which is where a missing channel surfaces.
4. Claims 5 and 6, the behavioural pair.

Steps 1–2 are independently useful: a valid, served, discoverable document that
is merely *not yet proven* to match behaviour.

## Files

| File | Change |
|---|---|
| `api/asyncapi.yaml` | new — the document |
| `internal/api/asyncapi.go` | new — `HandleAsyncAPI` |
| `internal/api/asyncapi_test.go` | new — the six claims |
| `internal/api/testdata/asyncapi-3.0.0.json` | new — vendored schema, test-only |
| `internal/api/router.go` | one route |
| `internal/api/middleware.go` | one `Link` in `discoveryLinks` |
| `internal/api/apicatalog.go` | one `service-desc` target |
| `main.go` | one `//go:embed`, passed through `Config` |
| `docs/api.md` | the new endpoint and what it describes |
| `CHANGELOG.md` | user-facing entry |

`santhosh-tekuri/jsonschema/v6` moves from indirect to direct, so `go.mod`
changes and the pre-commit hook regenerates the SBOM and
`THIRD_PARTY_LICENSES`.

## Rejected alternatives

| Alternative | Why not |
|---|---|
| Assemble the document in Go, like `apicatalog.go` | Cannot drift, but AsyncAPI is prose, examples and JSON Schema; as Go literals that fights both authoring and every AsyncAPI tool. Drift is a test's job here |
| Hybrid — hand-written messages, channels injected from the router | The file in the repo would stop being the document that is served, so the file alone is no longer reviewable |
| Parameterised channels over a type enum | Collapses `resource` into an eight-way union and makes the drift test expand the enum itself; also erases the list/detail resumability difference |
| `$ref` into the served OpenAPI document | Resolves when served, meaningless in the repo. One reference, two correct spellings — the `.json` trap again |
| Thin messages pointing at OpenAPI in prose | A consumer cannot validate a frame against it, which is most of the point |
| An embedded AsyncAPI renderer | A second vendored viewer with an SBOM, licence, CSP and Dependabot tail |
| Render the channels as a dashboard page | No new dependency, but frontend work in service of a document aimed at machines |
| Driving all 20 channels behaviourally | Sixteen of them are two functions; the enum comparison pins what varies, for a fifth of the streams |
| Extending OpenAPI instead | OpenAPI describes a response body. Frame names, cursor semantics and per-channel resumability have no slot in it — which is why four blocks of `text/event-stream` say so little |
| A `Link` header on each SSE response naming its channel | A fourth mapping to keep in step, for discoverability a client already has two routes to |

## Follow-ups, not in scope

- **The 16 resource endpoints should also declare `text/event-stream` in
  OpenAPI**, now that there is somewhere to point. A one-line
  `$ref: SSEEvent` content block per endpoint, plus a link to the channel.
- **Three cursor dialects is a design observation, not just a documentation
  problem.** Writing them down side by side makes the case for the log tail's
  timestamp cursor (Docker interleaves; ids must not move backwards) and against
  the metrics stream having no cursor at all. Worth a look once documented.
- **Detail-stream replay ineligibility** is a real limitation a client now
  learns about explicitly. Whether the ring should be queryable by type+id is a
  separate question this document only makes visible.
- **CloudEvents** (ledger, deferred) becomes tractable once channels and
  messages are named: the envelope mapping would be per-message, and this
  document is where it would be declared.
