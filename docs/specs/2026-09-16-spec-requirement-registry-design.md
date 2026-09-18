# Requirement registry and mutant catalog: design

## Problem

Four bugs found while auditing the OAuth and MCP surfaces, none visible from
coverage:

- `verifySHA256Challenge` could `return true` unconditionally and no test in
  `internal/oauth` failed. The wrong-verifier case sent a 14-character verifier
  that `validateCodeVerifier` refuses for its length first, and both refusals
  are `invalid_grant`.
- Matching RFC 8707 resource identifiers by prefix — audience confusion — broke
  no test. **This one is still unguarded on `main`:** reintroducing the prefix
  fallback in `effectiveResource` leaves the whole `internal/oauth` package
  green, and a probe confirms
  `effectiveResource("https://cetacean.test/resource/not-a-resource")` returns
  `"https://cetacean.test/resource"` with a nil error. Only two tests call the
  resolver and both are on the accepting side.
- The authorization code's binding to its client and to its `redirect_uri` are
  both implemented correctly, and deleting either left all 177 tests in
  `internal/oauth` passing — authorization code injection (RFC 9700 §4.5) with
  nothing guarding the guard.
- `requireModernProtocol` admitted any protocol version sorting after
  `2026-07-28`, so a client on a later revision was answered with four older
  ones it would then be refused on. Three e2e lanes drive `/mcp` and none of
  them sent anything but a known version.

Three of the four are the same fault: **a test existed, was named for the
requirement, ran, and passed, while the requirement was not enforced.** Only the
fourth is an absence of any test at all.

That distinction decides what is worth building, and an earlier draft of this
document got it wrong. It proposed a requirement registry — YAML listing each
requirement, tests declaring which they exercise, a gate failing on any
requirement without a claimant — and offered these four bugs as the
justification. The registry would have caught **one**. A claim is recorded when
a test passes; all three of the others had a passing, plausibly-named claimant
at the moment they were live. A gate that records "some test claims this
requirement and it passed" is coverage with a coarser unit, and coverage is what
already failed to notice.

The instrument that found all four was a human mutating the code and watching
whether anything went red. That act is the finding; everything else is
bookkeeping about it. So the bookkeeping is built around the instrument rather
than in place of it.

## What each piece does, stated narrowly

Three mechanisms, answering three different questions. None of them subsumes
another and none is a general correctness argument.

| Mechanism | Answers | Catches |
|---|---|---|
| Mutant catalog | Does our test for this requirement actually enforce it? | A test that agrees with itself |
| Requirement registry | Which requirements has nobody written a test for? | An absence |
| Citation sweep | Which specifications have we never written anything down about? | A blind spot |

The registry is the index the other two hang off: it owns the identifier, the
verbatim requirement text and the citation, and a mutant is attached to the
requirement it is supposed to break.

## Mechanism 1: the mutant catalog

A requirement may carry mutants — edits that should make its claiming tests
fail. The gate runs each one and **fails if the tests still pass**.

```yaml
- id: verifier-must-match-challenge
  level: MUST
  text: >-
    If the values are not equal, an error response indicating "invalid_grant"
    MUST be returned.
  url: https://www.rfc-editor.org/rfc/rfc7636#section-4.6
  mutants:
    - file: internal/oauth/server.go
      replace: "sum := sha256.Sum256([]byte(verifier))"
      with: "return true"
```

`go test -overlay` substitutes file contents from a JSON manifest without
touching the working tree, so a mutant run needs no repository copy, no
`git stash`, and cannot leave the tree dirty if it crashes. The runtime claims
file (below) says which tests claimed the requirement, so only those are re-run
per mutant rather than the whole package.

The property this buys is the one the earlier draft lacked: **every MUST-level
requirement has at least one mutant its own claimants kill.** Applied to the
four bugs above, it catches the first three — each is a deliberate weakening
that a hollow test does not notice, which is exactly what a mutant is.

Cost is compile-dominated: `./internal/oauth` runs in under a second but
recompiles per mutant, so mutants are batched per package. Twenty mutants is a
couple of minutes. It gets its own target and a pull-request job, not
`make check`.

**A mutant is not a unit test.** It is a claim about what our tests prove, and
it belongs with the requirement rather than beside the code, because the two
drift apart otherwise — which is how three of the four bugs survived.

### The discriminating-assertion rule

Cheaper than a mutant and it targets the exact shape of the first and third
bugs: several checks on one path share an error code, so the code alone cannot
say which check ran. `TestTheAuthorizationCodeIsBoundToItsClientAndRedirectURI`
already gets this right — it asserts the error *description* names
`client_id mismatch`, not merely that the response was `invalid_grant`. A
requirement whose refusal shares a code with its neighbours carries a
`discriminator:` naming the string that distinguishes it, and the claiming test
is expected to assert it.

### Generated mutants

`spec-gate mutants --generate <package>` swaps every comparison and logical
operator in a package — `<`↔`<=`, `==`↔`!=`, `&&`↔`||` — and runs the package's
whole suite against each. It reuses the executor above, so it inherits the
overlay, the build-failure check and the refusal to call a run of nothing a
kill.

It answers a question the catalog cannot: **a hand-written mutant proves a test
notices that edit, and nothing more.** The catalog's PKCE mutant is killed while
truncating the same comparison to eight characters survives, because nobody
wrote that one down. A generator does not need to have thought of it.

**It is not a gate, and should not become one.** An equivalent mutant — one that
cannot change behaviour — survives honestly, and no threshold separates it from
a hole. `verifySHA256Challenge`'s empty-argument guard is redundant with the
comparison below it, so its survivor is correct. The output is a list to triage;
a survivor worth keeping becomes a registry mutant, attached to the requirement
it breaks, where the gate can hold it.

Off-the-shelf tooling was tried first and does not fit this repository.
`gremlins` copies the module root per worker, and this root carries `node_modules`
and `.worktrees`; `ooze` symlinks instead, which `//go:embed` refuses to follow,
so every mutant fails to build and its runner counts a build failure as a kill —
it reports a perfect score on a package it never compiled. The executor here
already handles both, which is why generating into it beats adopting either.

## Mechanism 2: the registry

`internal/spec/registry/<family>/<document>.yaml`, one file per source
document. A requirement's identifier is its path:
`mcp/sep-2575/server-implements-discover`.

```yaml
source: SEP-2575
title: The stateless protocol core
revision: 2026-07-28
reviewed: 2026-09-16
url: https://modelcontextprotocol.io/specification/draft/basic/lifecycle

inventory:
  from: https://github.com/modelcontextprotocol/conformance/blob/main/src/seps/sep-2575.yaml
  count: 22

requirements:
  - id: server-implements-discover
    level: MUST
    text: Servers MUST implement server/discover.
    url:
      - https://modelcontextprotocol.io/specification/draft/server/discover
      - https://github.com/modelcontextprotocol/conformance/blob/main/src/seps/sep-2575.yaml

  - id: data-uri-on-not-found
    level: SHOULD
    text: Servers SHOULD include the requested URI in the error data field.
    deferred: >-
      Not implemented. The claiming test pins the field's absence, so adding it
      fails that test rather than passing quietly.

dismissed:
  client-populates-meta: A client requirement; Cetacean is the server.
```

`text` is the document's own prose, never a paraphrase, so the entry can be
diffed against the source by a human. `url` is a string or a list, inheriting
the file-level `url` when absent — the list form exists because a requirement
often has two homes that drift apart, and recording both makes a divergence
visible.

### Three states, not two

A requirement is in exactly one of:

- **claimed** — one or more tests exercise it. The normal state.
- **`deferred:`** — we knowingly do not satisfy it. Requires a prose reason
  **and** a claimant, because the claiming test pins the current non-conforming
  answer; fixing the behaviour then fails that test rather than passing
  quietly. This is what replaces the MCP conformance suite's expected-failures
  baseline. The earlier draft asserted this in prose while its gate rule
  allowed a deferred requirement with no claimant at all.
- **`gap:`** — implemented, transcribed, not yet tested. Spelled the same as
  `read_sweep_test.go`'s existing `"gap: "` convention and counted separately in
  the report.

### The lane, declared rather than inferred

A requirement the unit suite cannot reach carries `lane: e2e`. The report counts
a declared lane as **not run** rather than *uncovered*, and the static gate holds
the declaration to the claimants that exist: a requirement claimed only from
behind the build tag must declare it, and one a unit test reaches must not.

Inferring the lane from where the claim was written — which is what the first
cut did — makes the excuse automatic. Moving a requirement's only evidence into
the e2e tree then silently converts it from *exercised* to *not run*, and CI,
which never runs that tree, goes on passing. Declaring it makes that move a
reviewed diff instead.

Without the third state the format punishes writing a requirement down before
its test exists, which makes the blind spot the default rather than an edge
case. `read_sweep_test.go` grew that convention inside its excuse map for
exactly this reason; borrowing the spelling means it reads the same in both
places.

### The denominator

`inventory:` names where the document's full requirement set comes from and how
many there are. The gate then requires
**`requirements` + `dismissed` = `inventory.count`**.

This is the vacuity guard. Without it, the cheapest way to make the gate green
is to delete the requirement, and nothing notices a registry shrinking. With it,
deleting an entry fails, and a document that grows a requirement upstream fails
until someone transcribes or dismisses it. Dismissal is one line; transcription
is reserved for what we claim.

For MCP the inventory is exact and machine-readable, so the gate diffs
identifiers rather than counts. For RFCs there is no such list and `count` is a
number from a manual read, with `reviewed:` recording when.

## Mechanism 3: the citation sweep

Our own source cites **58 distinct RFCs and 7 SEPs**. The first cut registers
nine documents. The registry cannot say anything about the other fifty — and a
requirement nobody transcribed is a requirement nobody is told about.

The codebase already knows which specifications we think we implement, because
it names them in comments. So the sweep uses that as its inventory, the way the
existing gates use the running server as theirs. Every `RFC NNNN` and
`SEP-NNNN` appearing in `internal/`, `docs/`, `api/` or `test/` must be either
registered or listed in `registry/unregistered.yaml` with a one-line reason:

```yaml
RFC3339: Timestamp format, used via time.RFC3339. No behaviour of ours to test.
RFC9421: Named in a comment as the thing we deliberately do not implement.
```

Most dismiss in a line. The residue is the finding. It is also self-maintaining
in the right direction: adding code that cites RFC 7009 fails the gate until
someone registers or dismisses it, and that is precisely the moment they are
thinking about that document.

**The perverse incentive is real** — this taxes citing an RFC in a comment, and
the cheapest way to avoid the tax is to not write the citation. Compliance costs
one line, which is the only thing keeping it small. Worth watching rather than
worth solving in advance.

## Claiming a requirement

`internal/spec` embeds the registry and exposes one function:

```go
func Satisfies(t testing.TB, ids ...string)
```

It validates each identifier against the embedded registry immediately, so a
typo fails the test that made it. It records the claim from a `t.Cleanup` that
withholds it when the test failed or skipped, and writes to
`$CETACEAN_SPEC_CLAIMS/<pid>.claims` as `id<TAB>TestName` lines when that
variable is set.

**A skipped test therefore covers nothing.** This is the one property no static
`driven: TestFoo` list can have, and it is the registry's best idea rather than
an implementation detail: e2e lanes skip for real environmental reasons — no
Node, no tailnet, no installed plugin in a Docker-in-Docker engine — and a
requirement whose only evidence skipped on this machine has not been checked on
this machine.

Four rules, each of which came out of measuring rather than reasoning:

1. **Call `Satisfies` before any helper that registers a cleanup.** Cleanups run
   last-registered-first, so an assertion deferred by a helper registered
   earlier runs *after* the claim cleanup and is invisible to it. `harness.Up(t)`
   followed by `spec.Satisfies(t, …)` is the natural order and the wrong one.
2. **Only from a `_test.go` file.** The static gate cannot see `test/e2e/harness`
   or `fixtures`, which are ordinary `.go`; a claim there would be counted by the
   runtime gate and missed by the static one, and the two would disagree.
3. **String literals only, and no import alias.** The gate matches
   `<ident>.Satisfies` on the AST with no type information, so an alias breaks it
   in both directions.
4. **Identifiers and test names carry no tab or newline.** The claims line format
   has no escaping; a name containing either would split a record.

A panicking test still claims, because `testing` runs cleanups before recording
the failure. A panicking package is a broken run, and the report labels itself
partial when `go test` exited non-zero for any reason other than a claiming
test's own failure.

There is deliberately no `TestMain`. `internal/contract` uses one, and it works
there because its inventory and its recorder live in a single package; claims
here cross package *and process* boundaries — `go test ./...` and the
build-tagged e2e run are separate binaries — which a `TestMain` cannot
aggregate. One file per process, `O_APPEND`, is what makes that work with no
coordination.

## The gates, and where each actually runs

**Static.** Fails on a requirement with no claimant and no `deferred`/`gap:`
reason; an empty reason; a claim naming an unknown identifier; a non-literal
claim argument; a `deferred` requirement with no claimant; a `gap:` a claimant
contradicts; a document with no `inventory:` at all, or whose `requirements` +
`dismissed` does not match its `inventory.count`; a pointer in one entry's prose
at an identifier another document does not have; and a cited specification that
is neither registered nor dismissed; a requirement claimed only from behind the
`e2e` tag with no `lane:` declaration, and a declaration a unit-lane claimant
contradicts.

The last two of those are late additions, and each answers a hole the first cut
left. `inventory:` was optional, so the vacuity guard was opt-in and a new
document could simply carry no denominator. And a `gap:` had nothing that ever
retired it: the requirement's test could land, claim, and pass, while the report
went on counting it as one nobody had written. That is the mirror of the
deferred rule — a deferral needs a claimant, a gap needs the absence of one —
and it is what makes the third state self-closing rather than a one-line
silencer.

The pointer rule exists because of where the argument actually lives. A
dismissal is one line of prose, there are 838 of them against 347 transcribed
requirements, and most of them say *this is registered as `oauth/rfc9700/…`
instead*. That pointer is the only part of a dismissal a gate can follow at
all, and until it was checked, renaming a requirement silently orphaned every
entry leaning on it.

It **walks the filesystem, scoped to `git ls-files`** — not `go list` or
`packages.Load`. `go list ./test/e2e/` fails outright with "build constraints
exclude all Go files", so a package loader would silently omit the entire e2e
tree, which is the one thing this gate exists to reach. An unscoped walk is
equally wrong in the other direction: it finds 980 test files against
`git ls-files`'s 294, the difference being copies of `internal/oauth`,
`internal/mcp` and `test/e2e` inside `.worktrees/`, whose claims would mask
uncovered requirements here.

**This is not a check on the e2e suite.** It is a check on the registry's own
bookkeeping. It knows a literal was typed in a file it can parse; it does not
know the file compiles, the test is reachable, or anything is asserted.

**Mutants.** Its own target and a pull-request job. For each mutant, overlay the
edit and re-run the tests that claimed the requirement; fail if they pass.

**Runtime.** Aggregates the claims files and reports what a run exercised. Unit
suite by default, in seconds; requirements declaring `lane: e2e` are reported
**not run**, never *uncovered*. A full variant runs the e2e suite too, for the
release ritual. Both clear `$CETACEAN_SPEC_CLAIMS` before invoking anything and
pass `-count=1`, which is not optional — see the measurement table.

This is the only gate that answers *did the test pass*. A claim is withheld when
its test fails or skips, so a claimant that skipped for an environmental reason
leaves its requirement uncovered and the gate red, while `go test` itself stays
green. The static gate cannot see that and never will.

### CI

`make check` is not how this reaches CI. CI does not run `make check` — it runs
`go build`, `go test -race ./...`, the golangci-lint action and
`golangci-lint fmt --diff` as separate steps, and the only `make` target in any
workflow is `make sbom`. The static gate, the citation sweep and the report need
explicit steps in the `test` job, which already has Go and the module cache. The
report rides on that job's own `go test` step, which sets
`$CETACEAN_SPEC_CLAIMS` and gains `-count=1`. The mutant job is separate because
it is minutes rather than seconds.

Running only the static gate there was the first cut's mistake: it prints a
sentence that reads as a compliance claim — *"347 requirements across 38
documents, all accounted for"* — on evidence no stronger than a string literal
having been typed.

CI still never runs the e2e suite; that decision is unchanged. It is worth
adding `go vet -tags e2e ./test/e2e/...` on its own merits — e2e compile breaks
currently reach `main` unnoticed — which also upgrades the static gate's
guarantee for those files from "a literal exists" to "a literal exists in code
that compiles".

### The report

Never a bare ratio. The denominator is part of the sentence:

```
spec: 41 requirements across 7 documents (7 of 58 cited specifications)
  MUST      33 of 33 exercised, 31 with a killed mutant
  SHOULD     5 of  6 exercised
  gap        1 transcribed, no test yet
  deferred   3, each pinned by a test
  not run    9 (claimants are e2e-tagged; run spec-report-full)
```

`docs/` is published to the website, so a number from this has a plausible path
to being quoted at users. `33 of 33` without its denominator is a compliance
claim the suite cannot support, and the next person deciding whether to write a
harder PKCE test would read it and not bother.

## Against the grain: a hand-typed inventory

`internal/contract/doc.go` states the rule this design breaks:

> every test loops over an inventory derived from a source of truth — the
> router's source, the live MCP server, the OpenAPI spec — **never a hand-typed
> list**.

Every existing gate obeys it, and they are unfakeable because of it: a new route
appears in `contract.Routes()`, a new notification type appears in the server's
own acknowledgement, and the gate fails until someone drives or excuses it. The
person being gated cannot edit the inventory.

A specification cannot be interrogated that way, so this is the first hand-typed
inventory in the tree, and the exception has to be paid for rather than waved
through. Three things pay for it: `inventory.count` makes deleting a requirement
fail; the citation sweep derives *which documents* must exist from the source
rather than from a list; and for MCP the requirement set is generated from
upstream, so that family is not hand-typed at all.

What remains hand-typed is the RFC families' requirement text and counts. That
is a real weakening of the repository's standard and it is the reason the
registry is scoped to answering "what has no test" rather than being trusted as
a compliance statement.

## Relationship to the existing gates

None is migrated or subsumed. They answer a different question — *did we drive
everything the server has* — and they answer it from an unfakeable inventory.
This one asks *did we drive everything the document says*. Where both could
cover the same ground, the inventory-derived gate wins, because it cannot go
stale.

Two overlaps are real and get resolved rather than duplicated. SEP-2575's
subscription requirements overlap the notification gate in `mcp_stream_test.go`;
those requirements are claimed by the existing tests rather than given new
ones. And the three deferred answers are currently recorded in
`test/e2e/README.md` and again in `mcp_conformance_test.go`'s comments; the
registry becomes their single home and the README links to it, per CLAUDE.md's
"state it once where it lives".

## What the first cut populates

The citation sweep and `registry/unregistered.yaml` covering all 58 cited
documents — this is where the yield is, and it is one line per entry.

Then requirements that tests already prove, generated where possible:

- **MCP** — SEP-2575, SEP-2243, SEP-2549 and SEP-2164, **generated** from the
  conformance suite's `src/seps/*.yaml`, pinned to the revision
  `mcp_conformance_test.go` targets, with a target that regenerates and diffs.
  Those four declare 51 requirements upstream. Hand-copying a file that can be
  fetched is how transcription drift is created rather than avoided.
- **OAuth** — RFC 7636, RFC 8707, RFC 8414 and RFC 9728, hand-written, where
  immutability makes hand-writing defensible.

Mutants for the MUST-level OAuth requirements, starting with the three the
audit found: the PKCE verifier comparison, the resource-identifier match, and
the authorization code's client and redirect bindings.

**RFC 7515 and RFC 7638 are out of the first cut.** Appendix A.3 and §3.1
contain zero RFC 2119 keywords — they are worked examples, not clauses. `level:`
is unanswerable for them and `text:` would quote an example as if it were
normative. The vector tests stay; they are simply not requirements.

One entry needs deciding before it is written. The conformance check
`sep-2575-request-meta-invalid-missing-client-capabilities`, which expects
`-32602` where mcp-go answers `-32021`, appears in the suite's
`traceability.json` and in **no requirements file upstream**. It is a scenario
assertion with no normative text behind it, so it is not a requirement to defer
— it is a disagreement to raise upstream.

## What this does not do

- **It is not a correctness argument.** Three of the four bugs that motivated it
  would have been reported as covered by the registry alone. Only the mutant
  catalog addresses them, and only for requirements someone wrote a mutant for.
- **It finds no unknown unknowns beyond the sweep's reach.** A requirement in a
  document we have never once cited is invisible, and nothing short of reading
  the document changes that.
- **Our own documentation is permanently out of scope.** `docs/configuration.mdx`
  is canonical for the configuration table and CLAUDE.md forbids restating it;
  a registry whose `text` is verbatim prose would be a second copy of it. The
  registry covers external specifications with normative language. The earlier
  draft offered `source: docs/configuration.mdx` as an example of cheap
  expansion, which was exactly backwards.
- **No generated identifier constants**, no committed coverage artifact, and no
  Gherkin. The failure mode here is tests agreeing with themselves, which a step
  layer does not prevent.

## Known weaknesses

**Transcription drift** in the hand-written RFC families. `text` is verbatim and
`url` carries every home a requirement has, so it is diffable; nothing detects
it automatically. Deliberately manual for now. Generation closes it for MCP,
where the source moves fastest — those URLs point at `/draft/`, so drift there
is certain rather than hypothetical.

**The static gate is satisfiable by a stub.** A claim at the top of a
three-line test turns CI green forever. The mutant catalog is what makes a claim
mean something, and it only runs where a mutant was written.

**`dismissed:` is the biggest unguarded surface, and the `inventory.count` guard
does not reach it.** The denominator stops a requirement being *deleted*; it
does not stop one being *reclassified*, because a dismissal counts toward the
same total. Moving `oauth/rfc7636/verifier-must-match-challenge` from
`requirements:` to `dismissed:` and deleting its one claim line takes the
strongest MUST in the registry — and its mutants with it — off the compliance
surface, with every gate green and the count untouched. For the RFC families the
count is hand-typed anyway, so decrementing it works as well. There are 838
dismissals against 347 transcribed requirements; for OAuth 2.1 alone, 72 against
29. That prose is where the compliance argument actually lives, the pointer rule
checks the one part of it a gate can follow, and the rest is review.

**`deferred` and `gap:` will accumulate.** `excusedReadRoutes` holds nine
entries and has already grown its own `"gap: "` sub-convention; the e2e README
carries a ten-item Deferred list. This adds a third such surface and the
controls are weak: a required reason, a required claimant for `deferred`, the
absence of one for `gap:`, and each gap named rather than counted in the report.
Worth revisiting with a cap or a dated review if it grows.

**Nobody owns writing an entry.** Adding a feature that implements RFC 7009 with
no registry file fires nothing — except the citation sweep, if the code cites the
RFC. That is the single reason the sweep is in the first cut rather than
deferred, and it is still weaker than a real ownership rule. If this survives a
quarter, it needs a line in CLAUDE.md's Conventions.

## Measured, so it is not re-litigated

Established empirically while designing this; recorded so the next reader does
not have to re-derive them.

| Claim | Result |
|---|---|
| `t.Cleanup` sees `t.Failed()`/`t.Skipped()`; a passing parent does not claim when a subtest failed | Holds, including for parallel subtests |
| A panicking test claims anyway | True — cleanups run before the failure is recorded |
| A failing cleanup registered *before* `Satisfies` is invisible to it | True — LIFO ordering |
| `go/parser` applies no build constraints | True; all 294 tracked test files parse, 35 of them behind `//go:build` |
| `go list ./test/e2e/` | Errors: "build constraints exclude all Go files" |
| `O_APPEND` line writes from 16 processes | No torn lines on APFS or overlayfs, to 70 KB |
| `go test` result caching poisoning the report | **Occurs.** `CETACEAN_SPEC_CLAIMS` is read through `os.Getenv`, so its value joins the cache key: a repeat run with the same directory and unchanged code replays and records nothing. `-count=1` is what every claims run needs |
| Importing `testing` outside `_test.go` | No flags registered; ~199 KB binary growth; nothing in the production tree imports `internal/spec` |
| The claims-file write under the repo's lint config | **Fails** `make check`: G302 needs `0o600`, and G703 fires on a path derived from `os.Getenv`; `.golangci.yml` excludes G304 and G306 but neither of these |
| `//go:embed registry` | Silently drops `_*` and `.*` entries at every level; an empty directory is a build failure, so the first commit must carry a YAML file |
