# Publishing the compliance report: design

## Problem

The registry knows what it knows and says it only to whoever runs `make`. A
reader outside the repository — someone deciding whether to trust this
authorization server — has no way to see any of it.

`spec.Observed` closed the part of that gap which was about evidence: a claim
now carries what the test watched the server do, and `make spec-transcript`
prints the requirement beside the answer. What remains is transport, and one
number that says how far it has to carry:

```
312 exercised by unit, 24 not run, 1 gaps, 30 deferred, 0 uncovered
16 of the 312 exercised recorded what the server answered
```

Sixteen. A page published today would be five percent evidence and ninety-five
percent checkmarks, which is the shape of a compliance page nobody should
believe. The publication mechanism is worth building now anyway, because it is
what makes the sixteen visible and therefore worth growing.

## One model, four renderings

Every output comes off one `Report` value, assembled once:

| Rendering | Audience | Where |
|---|---|---|
| summary | whoever ran `make` | stderr, exists |
| transcript | the same person, reading | stdout, exists |
| JSON | the other three, and anyone recomputing | `--json <path>` |
| SARIF | maintainers, in pull requests | `--sarif <path>` |
| HTML | everyone else | the website, from the JSON |

The alternative — a template in Go for the page and a second traversal for the
SARIF — puts the same facts in three places and lets them disagree. The page is
the one output this repository cannot test end to end, so it gets the least
logic: Astro reads a data file, exactly as the error reference already reads
`src/data/errors.json`.

## Only one half of it needs a test run

Most of what the page is for is a property of the registry, not of a run. The
denominators, the deferrals and their reasons, the gap, the lanes, the mutant
catalog, the statements no entry accounts for, and — because the static scan
records a file and a line per claim — which tests claim a requirement at all.
None of that needs `go test` to have happened.

Only three things do: which of those claiming tests passed, what they observed,
and therefore each requirement's `evidence.state`.

So the claims directory becomes optional. Without records in it, `suites` is
empty and every requirement's `evidence` is absent; the rest is filled. That
single switch is what lets the website generate its own data file in
`sync-assets`, beside `errors.json` and by the same mechanism:

```
(cd .. && go run ./scripts/spec-gate report --json website/src/data/compliance.json)
```

`spec.ReadClaims` globs a directory; a missing one yields no records rather than
an error, so the same command serves both cases. `predev` and `prebuild`
already run `sync-assets`, so a local `pnpm --filter website dev` renders the
denominators, the deferrals and the gaps — which the Known Weaknesses section
below argues are the part doing the most work — and only the evidence columns
degrade to "not built in this environment".

Both new renderings write to a path rather than to stdout: the summary already
owns stderr and the transcript stdout, one invocation has to produce both
files, and its exit code has to keep meaning what it means.

This is also what keeps the page off a cross-workflow artifact handoff.
`pages.yml` is a separate workflow from `ci.yml`: it cannot `needs:` a job
there, reaching an artifact across runs means `actions/download-artifact` with
`run-id` and an `actions: read` scope the workflow deliberately does not hold,
and both workflows trigger on the same push, so the page build would have to
find and wait for the run of its own SHA. Instead `pages.yml` runs the unit
suite itself before building. It already sets Go up for `dump-errors`; the cost
is a few minutes on a job that installs Chromium anyway, and the gain is that
the page's evidence is from the commit the page is built from, with nothing to
poll.

**The data file is not committed**, for the reason the registry's own design
gives: a bot commit rewriting a generated file on every push is what that rule
is about. `website/src/data/compliance.json` joins `errors.json` in
`website/.gitignore`.

## The model

```jsonc
{
  "schema": 1,
  "generated": "2026-09-19T12:00:00Z",
  "commit": "78821075",
  "suites": ["unit"],
  "counts": { "documents": 38, "requirements": 367, "dismissed": 876 },
  "citations": { "specifications": 63, "registered": 36 },
  "documents": [{
    "key": "oauth/rfc7636",
    "source": "RFC 7636",
    "title": "Proof Key for Code Exchange by OAuth Public Clients",
    "url": ["https://www.rfc-editor.org/rfc/rfc7636"],
    "inventory": {
      "count": 17,
      "sections": ["4"],
      "unheld": ["code_verifier REQUIRED.", "…"],
      "unheldFrom": "baseline"
    },
    "dismissed": 8,
    "requirements": [{
      "id": "oauth/rfc7636/missing-challenge-refused",
      "level": "MUST",
      "text": "If the server requires Proof Key for Code Exchange (PKCE) by OAuth …",
      "url": ["https://www.rfc-editor.org/rfc/rfc7636#section-4.4.1"],
      "lane": "",
      "entry": { "file": "internal/spec/registry/oauth/rfc7636.yaml", "line": 62 },
      "mutants": [{
        "file": "internal/oauth/server.go",
        "replace": "…\"invalid_request\",\n\t\t\t\"code_challenge is required\")…",
        "with":    "…\"invalid_grant\",\n\t\t\t\"code_challenge is required\")…"
      }],
      "claimants": [{
        "test": "TestTheMissingChallengeRefusalIsExplained",
        "file": "internal/oauth/conformance_test.go",
        "line": 1332
      }],
      "evidence": {
        "state": "satisfied",
        "tests": [{
          "test": "TestTheMissingChallengeRefusalIsExplained",
          "observations": ["no code_challenge -> error=invalid_request, no code issued"]
        }]
      }
    }]
  }]
}
```

`citations.specifications` is what the tree cites and `citations.registered` how
many of those the registry has a document for; `counts.documents` is the
registry's own size, and the two differ because two registered documents are
cited nowhere. The original shape of this field said `{cited, registered}` and
read as "36 documents in the registry", which is wrong by two.

`deferred` and `gap` appear on a requirement only when set, carrying their
reason. `evidence.state` is one of `satisfied`, `deferred`, `gap`, `not-run`,
`uncovered` — the report's existing vocabulary, invented nowhere new — and the
whole `evidence` object is absent when no run contributed, which is what stops
a claims-free build from rendering 367 requirements as uncovered.

`mutants` is the catalog, not results; see *Deliberately not in this cut*.

`schema` is there because the moment this is attested it becomes an interface.

Two producers of this file means its bytes have to be stable: `Document.Dismissed`
is a `map[string]string` and `Citations` returns a map, so both need sorting on
the way out or every artifact diff churns on nothing.

## What has to be built to produce it

1. **Line numbers in the loader.** `internal/spec` decodes YAML into structs and
   throws positions away. SARIF requires a location on every result, and the
   page wants to link a requirement to its entry. `URLs` has a custom
   `UnmarshalYAML`, so threading `yaml.Node` through the types would touch it:
   decode the file into a `yaml.Node`, call `node.Decode(doc)` as today, then
   walk the `requirements` sequence separately and key each mapping's line on
   its `id` scalar. No struct tag changes, no unmarshaler changes. Do this
   first; everything else waits on it.

   The embed gives paths like `registry/oauth/rfc7636.yaml`, and both SARIF and
   the page need `internal/spec/registry/oauth/rfc7636.yaml`. The prefix is
   reconstructed once, at the loader, or every alert is unclickable.

2. **The claimant join.** The static scan carries `File` and `Line` per claim
   and names the claim by its enclosing `*ast.FuncDecl`; the claims file names
   it by `t.Name()`. For a claim written inside a `t.Run`, those are
   `TestDecodeClientCertBase64Tolerances` and
   `TestDecodeClientCertBase64Tolerances/non-zero_pad_bits_are_accepted`, which
   never match. This is not hypothetical: `spec-vet` explicitly supports the
   form, and six claims in the tree are written that way — they would silently
   lose their file and line.

   The join is the same prefix match `claim.go`'s `under` already makes in the
   other direction: `runtime == Func || strings.HasPrefix(runtime, Func+"/")`.

3. **Extract statements, not a count.** `baseline.yaml` commits the unheld
   statements rather than a number, and its header says why — a count says
   nothing about which ones. The report carries the statements through and lets
   the page count them; `unheldFrom` says whether they came from a live
   `spec-extract` run or from the baseline, because nothing in CI runs
   `spec-extract` (it needs the network and is deliberately outside `make
   check`), so in practice it is always the baseline.

   Only two of the 38 documents declare `inventory.sections`, which is what
   `spec-extract` needs to read one at all. This column is two rows of content
   and thirty-six blanks, and the page has to say so rather than leave the
   blanks looking like zero.

4. **`--json`** over the merged whole, **written before the exit**. `runReport`
   returns an error on any uncovered requirement, and a failed test withholds
   its claim — so one flake turns `satisfied` into `uncovered` and the command
   fails. The file must be complete on disk before that return, and uploaded
   with `if: always()`, or the artifact is missing exactly when someone wants to
   read it.

5. **`--sarif`** over the same value, in the same invocation.

## The page's contract

This is the part that decides whether the artifact is evidence or marketing,
and it is a decision about content rather than code.

The page publishes, with no less prominence than the satisfied list:

- every deferred requirement and its reason — currently 30, of which 19 are
  MUSTs and 16 are OAuth's
- beside each deferral's reason, **what the server does instead**. A deferred
  requirement is pinned by a claiming test, and nothing stops that test calling
  `spec.Observed` — `writeTranscript` filters on having observations rather
  than on state, so a deferral's would already print. None of the sixteen is on
  a deferred requirement today, so the column renders empty for all thirty. It
  belongs in the contract anyway: it is the single most credible thing this
  page can carry, a reason alone wastes it, and thirty empty cells beside
  thirty reasons is the clearest statement of what to write next.
- each document's `inventory.count` against what `spec-extract` reads out of the
  document, and the unheld statements — for the two documents where that is
  answerable, and marked unanswerable for the other thirty-six
- how many requirements carry a mutant at all — 187 of 367 — and how many of
  those mutants are more than a disabling one
- how many rest on exactly one test
- how many are `lane: e2e`, and that CI never runs that lane
- the observation ratio, whatever it is

The lane figure needs its own sentence. The registry has 25 `lane: e2e`
requirements but the summary's *not run* is 24, because one of the 25 is also
deferred and `summarise` classifies deferral first. Printed a few lines apart
with no explanation, the two do not add up and a reader who notices reads it as
an error.

Never a bare ratio, for the reason the summary already refuses one: a number
without its denominator reads as a compliance claim no suite here supports. A
page showing only passes is discounted entirely by the one reader it is for.

## SARIF, and what it can carry

GitHub's importer does not implement `result.kind`, so a result marked `pass`
becomes an alert. Publishing satisfied requirements as SARIF would produce
several hundred false alerts, and the Security tab needs write access to read
anyway. SARIF therefore carries findings only, for maintainers, in pull
requests:

| state | result | level |
|---|---|---|
| `deferred` | yes | warning |
| `gap` | yes | note |
| `uncovered` | yes | error |
| `satisfied`, `not-run` | omitted | — |

`uncovered` is in the table for completeness and will never appear: the report
already exits non-zero on one, so CI fails before it uploads anything. The
honest description of this surface is *deferrals and gaps* — around thirty-one
results against limits of 25,000 per run.

A row for "document above its extract baseline" belongs here on paper and is
left out, because nothing in CI runs `spec-extract`: it could never fire, and a
row that cannot fire teaches a reader of this table the wrong thing about the
others.

Each requirement becomes a `reportingDescriptor`: `id` is the full identifier,
`shortDescription` the first sentence of the requirement text, `fullDescription`
the whole of it, `help.text` the reason or the mutant summary, and `helpUri` the
requirement's own URL. `partialFingerprints` is a digest of the full identifier
rather than of the location, so moving an entry within its file does not close
one alert and open another.

## CI

Two workflows touch this, and neither waits on the other.

**`ci.yml`'s `test` job** already sets `CETACEAN_SPEC_CLAIMS` and runs the
report. It gains `--json` and `--sarif`, uploads the JSON as an artifact with
`if: always()`, and uploads the SARIF to code scanning. That
upload needs `security-events: write`, granted on the job rather than the
workflow, and it needs a fork guard: `ci.yml` runs on every `pull_request`, a
fork PR gets a read-only token no `permissions:` block can widen, and without
the guard every external contribution ends on a failed step. The repository is
public, so code scanning itself costs nothing.

**`pages.yml`'s `build` job** runs `go test ./...` with `CETACEAN_SPEC_CLAIMS`
set before `pnpm --filter website build`, so `sync-assets` writes a
`compliance.json` with evidence in it. A failing suite fails the page build,
which is correct: a compliance page assembled from a commit whose suite does not
pass is the exact artifact this design exists to avoid. The job's
`timeout-minutes: 10` already covers a Chromium install and an Astro build and
will not also cover a cold Go build and the unit suite; it goes up, and the Go
module and build caches `ci.yml` keeps come with it.

Its path filters need `internal/spec/**`, `scripts/spec-gate/**` and
`scripts/spec-extract/**` added, because a commit that registers a requirement
touches none of the paths currently listed and would leave the page at whatever
the last docs change produced. A commit that only adds a claim in a `*_test.go`
still does not rebuild the page. Widening the filter to every Go test file
would rebuild the site for most Go commits; instead the page carries `commit`
and `generated` and states which commit it describes, and the residue is that
the observation count can lag by a few commits.

There is no third job. The original design had a `compliance` job whose only
work was merging the test report with the mutant results, and with those cut
there is nothing to merge.

The "not built in this environment" fallback covers local development, where
the evidence half is genuinely absent. The published build generates both
halves in one job, so an empty `suites` there is a failure, not a state to
render.

## Deliberately not in this cut

- **Mutant kill results.** `"killed": 3` beside `"written": 3` looks like a fact
  about the commit and is a constant: `runMutants` collects every failure and
  exits non-zero if any exist, and `spec-mutants` is a CI job, so on any commit
  that gets as far as publishing, `killed == written` for every requirement,
  always. The field can only print the catalog back. Worse, `kill` flattens
  three outcomes into one `error` — survived, did not compile, no claimant
  matched `-run` — and a `--json` that did not separate them would render a
  broken catalog entry as an unkilled requirement, which is less honest than
  saying nothing. What the page contract actually asks for is a static property
  of the catalog, readable without running anything. Making the page wait on a
  minutes-long job for a field with no information in it is the cost of getting
  this wrong.
- **Attestation.** `actions/attest` over the report JSON is what turns "a file
  the maintainer could have edited" into a signed statement tied to a workflow
  and a commit. It is worth nothing until the content is worth signing, and at
  sixteen observations it is not yet.
- **An HTML renderer in Go.** The website owns rendering.
- **e2e observations.** Most observations today come from `httptest` handler
  calls rather than wire traffic, which is weaker evidence than the same
  exchange against the running binary. Recording them from the e2e lane is the
  right end state and a separate change — and CI runs that lane never, which is
  a decision to revisit on its own merits rather than smuggle in here.

## Known weaknesses

**Publication does not make a claim true.** Everything here is transport. What
makes the page worth reading is the observations and the denominators — and of
those, the denominators do the most work, because they are the only part a
reader can use to discount the rest. The mutants would be the third, and this
cut publishes only the catalog: a reader can see what edit a requirement's
tests are supposed to refuse, not that they refused it.

**The observation is still self-reported.** A test chooses what to record, so a
test that asserts loosely records loosely. `Observed` moves the lie from "a test
passed" to "a test says it saw this", which is further than it sounds but is not
proof.

**The page is built by a second test run.** `pages.yml` running the suite itself
buys independence from `ci.yml` at the price of the same tests running twice per
push to `main`. If that ever costs more than it saves, the alternative is the
cross-workflow artifact and everything in *Only one half of it needs a test run*
that argues against it.

**`schema: 1` will be wrong eventually.** Attesting a shape freezes it. Adding a
field is safe; changing what `evidence.state` means is not, and there is nothing
here that would catch the second.
