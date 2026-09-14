# Coverage campaign: design

## Problem

The ask is a test suite covering "all user paths and every cross-combination of
states". Taken literally that is unreachable — 135 route registrations × 5 auth
modes × 4 operations tiers × arbitrary ACL policies × 4 content types × cluster
states is millions of cases, and a suite that size is too slow to run, which
means it stops being run, which is worse than not having it.

The tractable form of the same intent is to **sweep a small set of invariants
across a machine-generated inventory of the surface, and measure the result**.
Instead of hand-writing cases per endpoint, we write N invariants and one driver
that applies each to every route, tool and type it applies to. Coverage becomes
self-enforcing: a route added without coverage fails the inventory test rather
than silently going untested.

The handover this follows sets out that shape and a catalogue of 17 invariants
(`docs/plans/2026-09-11-coverage-campaign-handover.md` — a local working
document, since `docs/plans/` is gitignored). Its account of what exists today,
however, is measured from `test/e2e` alone while reading as though it were
repository-wide, and acting on it unexamined would mean rebuilding existing
coverage in the slowest tier available.

### What is already covered

| Handover claim | Actually |
| --- | --- |
| Pagination: zero coverage | `internal/api/pagination_test.go` — 25 tests, including RFC 7233 `Range` |
| Atom: zero coverage | `atom_handlers_test.go` and `atom_handlers_integration_test.go` |
| I3/I4, ACL filtering and leaks | `acl_integration_test.go` covers all eight list types plus detail denial; also `acl_sse_test.go`, `profile_acl_test.go` |
| I5, the `Allow` header | `allow_test.go` — 15 tests over `setAllow`/`setAllowList` |
| I12, operations tiers | **PR #225** — `TestEveryOperationIsGatedAtItsDeclaredTier` walks all 53 OpenAPI `operations-level` badges against the assembled router at all four levels, mutation-verified |
| Route/spec drift | `TestEveryReadEndpointMatchesSpec` already walks spec → router and validates response schemas |

The repository holds 2,032 Go tests across 299 files, and an established
practice for exactly this class of work: the E-block ledger
(`docs/plans/web-standards-ledger.md`) and drift-check designs such as
`docs/specs/2026-09-09-mcp-catalog-drift-check-design.md`. The house pattern is
to derive from the OpenAPI spec, drive the assembled router, and prove the test
can fail by mutating the product.

### What is genuinely missing

- **The router → spec direction.** Nothing parses `internal/api/router.go`, so a
  route that is registered and never documented is invisible. The spec → router
  direction is covered; the loop is open.
- **Cross-transport agreement.** Nothing imports `internal/api` and
  `internal/mcp` together, so no test can assert that the two transports
  describe one cluster identically. The handover predicts a live divergence
  here: REST resolves a service by ID only (`cache.GetService`) while MCP
  resolves ID-then-name (`cache.ResolveService`), so `GET /services/shop_web`
  404s where the MCP read succeeds.
- **Fuzzing.** The repository has zero fuzz targets, against eight parsers that
  consume attacker-controlled input.
- **Measured coverage.** No baseline exists, in either tier.

## Decision

Four decisions, each with its cost stated.

### 1. Invariants run in-process by default; the e2e harness keeps only what needs a process

`api.RouterConfig` already accepts `MCPHandler` and `OAuthRoutes`, so a single
`httptest.Server` can mount the real REST router, the real MCP handler and the
real OAuth 2.1 endpoints over one seeded `cache.Cache`. That reaches further
than the handover assumes — it puts even the OAuth invariants (DCR, CIMD, PKCE,
refresh rotation, theft detection) in the fast tier, and most auth modes with
them: `none`, `headers`, `cert` via the RFC 9440 `Client-Cert` fallback, and
OIDC bearer validation all compose in-process.

The e2e harness keeps what is genuinely about the shipped artifact: the five
providers end to end, real TLS termination, `tailscale`, real SSE over the wire,
cluster state, and the coverage delta described below.

**Cost:** an in-process router is a library, not the binary. `main.go` wiring and
config discovery stay the harness's job, and this design does not pretend
otherwise.

**Why not the harness for everything:** `test/e2e/README.md` states the suite
never runs in CI and takes minutes. Putting the campaign's centre of gravity
there reproduces the exact failure the handover warns about on its first page —
coverage that only fires when somebody remembers to run it.

### 2. `internal/contract` holds the whole catalogue

One package, no build tag, running under `go test ./...` and in CI. It imports
`internal/api`, `internal/mcp`, `internal/cache` and `internal/acl`; nothing
imports it, so there is no cycle.

**Cost, accepted deliberately:** `internal/api`'s `newSeededTestRouter` is
test-only and cannot be imported, so `internal/contract` builds its own seeded
cluster. Two fixtures that could disagree is a real hazard. It is mitigated by
keeping the contract fixture minimal and deriving every assertion from the
inventories and the OpenAPI spec rather than from fixture contents — a test that
asserts against what the fixture happens to hold is the one that rots quietly.

### 3. The first slice is measurement, inventories, cross-transport and fuzzing

Delivered before reassessing: Step 0's coverage baseline in both tiers, the
three inventories with the router → spec drift check and the excused-route
allow-list, invariants I1 and I2, and the eight fuzz targets.

**Why this slice:** it is the part that is cheap, self-enforcing and most likely
to surface real defects — the handover already predicts I1 fails on arrival. The
defect list is the campaign's actual product, and this ordering produces it
early rather than at the end of a multi-week branch.

### 4. Branched on top of `test/e2e-harness`

That branch is 25 commits ahead of `main`, zero behind, reviewed and merge-ready
but unmerged, and Step 0's `GOCOVERDIR` plumbing edits `test/e2e/sut`. Stacking
avoids blocking on a merge.

**Cost:** the campaign's pull request carries 25 unrelated commits until its base
lands, and review of the two is entangled until then.

## The world

`internal/contract/world.go` builds one seeded `cache.Cache` holding all eight
resource types plus a stack, a converging service and a permanently failing one
— mirroring the shape of `test/e2e/fixtures`' baseline, so a finding here
translates to a finding there. Over that cache it wires:

- the real `api.NewRouter`, with `MCPHandler` and `OAuthRoutes` populated from
  `internal/mcp`, so all three surfaces answer from one `httptest.Server`
- an `acl.Evaluator` per persona, built from the four personas already defined
  in `compose.dev-auth.yaml` rather than a fifth definition of the same people
- a swappable `auth.Provider`, so an invariant can be re-run per auth mode

## The inventories

Three, each derived from a source of truth, none typed by hand.

| Inventory | Source | Mechanism |
| --- | --- | --- |
| REST routes | `internal/api/router.go` | `go/ast` over the `mux.Handle`/`mux.HandleFunc` string literals, yielding method and path pattern |
| MCP catalog | the live server | `tools/list`, `resources/list` and `prompts/list` against the mounted handler |
| Spec | `api/openapi.yaml` | parsed as `internal/api`'s helpers already do |

Two invariants over them, both new:

- **`TestEveryRegisteredRouteIsDocumented`** — the direction nothing covers. A
  route registered and never documented fails. The accounting alone is worth
  doing: 135 registration sites against 53 badged operations is a gap that has
  never been reconciled, and some of it is legitimate (suffix variants, SPA
  fallback, meta endpoints) while some may not be.
- **`excused.go`** — an explicit `route → reason` allow-list. The test fails if a
  route is neither exercised by a sweep nor excused. The allow-list is the honest
  record of what is not covered; reasons must be real, and "not yet" is not one.

## The first slice's invariants

**I1 — cross-transport readability.** For every resource in the fixture and every
persona: readable over REST iff readable over MCP. Expected to fail on arrival on
service-by-name resolution. A failing invariant is the deliverable; it is
recorded, not worked around.

**I2 — `find` and `describe` agree**, for every type in
`describableResourceTypes` rather than the single type and single instance
covered today. Driving from that map means a newly describable type cannot slip
past.

## Fuzz targets

Every entry point named is unexported, so each `FuzzX` lives in the package that
owns it. **No product change, and no export widened to make a target reachable.**

| Target | Package | Property beyond never panicking or hanging |
| --- | --- | --- |
| `FuzzForwardedNodes` | `api` | every returned node parses as an address, or is dropped |
| `FuzzResolveClientIP` | `api` | no input makes an untrusted peer report as trusted |
| `FuzzDecodeClientCert` | `auth` | no input yields a certificate the CA did not sign — paired with a real verification step |
| `FuzzApplyJSONPatch` | `api` | a failed operation leaves the input map untouched |
| `FuzzFilterCompile` | `filter` | bounded evaluation time; expr-lang over a query parameter is the denial-of-service surface |
| `FuzzParseDockerLogs` | `logs` | bounded allocation |
| `FuzzMatchResource` | `acl` | a pattern never matches outside its glob |
| `FuzzMCPEnvelope` | `mcp` | always a well-formed JSON-RPC error, never a 500 |

Corpora are seeded from inputs that already produced bugs: `for=unknown`, the
IPv4-mapped hop `::ffff:10.0.0.2`, a duplicated `Client-Cert`, and a
colon-wrapped base64 DER.

Seed corpora execute as ordinary unit tests under `go test ./...`, so the seeds
become CI regression tests on day one at no cost. The `-fuzztime` pass is a
separate `make fuzz` target and an optional bounded CI job.

## Measurement

Two numbers, because one would mislead.

- **Repository baseline** — `go test -cover ./...`, per package. Given the
  existing 2,032 tests this is the honest headline.
- **E2E delta** — `go build -cover`, `GOCOVERDIR` threaded through
  `sut.buildEnv`, merged with `go tool covdata`. What matters is what the harness
  reaches that `go test ./...` cannot: `main.go` wiring, config discovery,
  provider startup, real TLS. The harness's absolute percentage is not a
  meaningful number and will not be reported as one.

Two things to verify rather than assume: that `Process.Stop`'s SIGINT actually
produces coverage meta files — `main.go` uses `signal.NotifyContext`, so it
should, but the file is dense with `os.Exit(1)` paths that would not — and that
a coverage-instrumented build leaves the tree rebuildable, since the harness
hardcodes `./cetacean`.

**Stated in the report:** line coverage measures what executed, not what was
asserted. A route can be fully covered by a test that checks only a status code.
It locates zero-coverage regions; it does not declare victory.

## What the open pull requests change

Six pull requests are open against surfaces this campaign sweeps.

| PR | Overlap |
| --- | --- |
| #225 `test/operations-level-parity` | Implements I12 already, and more strongly than the handover describes |
| #221 `fix/e-block` | Document caching, feed `Link` headers, node-label tier — moves I9 and I10 |
| #220 `feat/csrf-protection` | Cross-origin write refusal — changes every write endpoint's expected behaviour |
| #207 `feat/persist-dcr-registrations` | Inside I17 |
| #226 `feat/at-jwt-token-profile` | Inside I17 |
| #198 `feat/acl-labels-port` | Label-derived grants — changes the model I3 through I6 sweep |

The rule: invariants are written against the behaviour on `main`. When one of
these lands and moves an invariant, the invariant moves with it — the product is
never adjusted to keep a test green, and a test is never weakened to accommodate
a change it was written to catch.

## Deferred to later slices

I3 through I11 and I13 through I17, the cluster-state matrix, and the `tailscale`
provider. The second DinD node the state matrix wants is materially more
resources and will be asked for rather than assumed.

## Alternatives considered

**Follow the house pattern instead of a new package.** Single-transport
invariants would live in `internal/api` beside `TestEveryReadEndpointMatchesSpec`,
reusing `newSeededTestRouter` — which #225 just made option-configurable for
exactly this. Less new machinery and a shape reviewers already know. Rejected in
favour of one readable home for the catalogue; the fixture-duplication cost this
incurs is mitigated above.

**Everything at the e2e tier, as the handover describes.** Highest fidelity: it
proves the shipped artifact rather than a library. Rejected because that suite
never runs in CI, so the invariants would only fire on demand, and because it
would rebuild existing in-process coverage for ACL, Atom, pagination and `Allow`.

**One invariant, two drivers.** Write each sweep against an interface satisfied
by both an `httptest` server and the e2e SUT. Maximum coverage per invariant
written. Rejected because the abstraction must hide real differences — no TLS, a
differently composed middleware chain, no real Docker — and that seam is exactly
where an assertion that cannot fail would hide.

## Non-goals

- **No product changes.** Defects found are recorded with a reproduction and
  handed to the fixing phase; the known defects in the e2e findings handover
  stay out of scope.
- **Not a replacement for the e2e harness.** The two tiers answer different
  questions and both are kept.
- **Coverage percentage is not a completion criterion.** The defect list is.

## How we know the tests can fail

Every invariant added must be provable by breaking the product and watching it
fail, in the style #225 established — seven deliberate mutations, all caught.
Where a mutation is impractical, the reasoning for why the assertion can fail is
stated in the commit message. Three assertions that could not fail were already
found among the existing 29 e2e cases; the discipline exists because the failure
mode is real.

When a mutation requires rebuilding, **rebuild `./cetacean`** — the harness runs
the binary, and editing source without rebuilding produces a meaningless pass.
