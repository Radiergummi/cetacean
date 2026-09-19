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
| JSON | the other three, and anyone recomputing | `--format=json` |
| SARIF | maintainers, in pull requests | `--format=sarif` |
| HTML | everyone else | the website, from the JSON |

The alternative — a template in Go for the page and a second traversal for the
SARIF — puts the same facts in three places and lets them disagree. The page is
the one output this repository cannot test end to end, so it gets the least
logic: Astro reads a data file, exactly as `/api/errors` already reads
`src/data/errors.json`.

## The model

```jsonc
{
  "schema": 1,
  "generated": "2026-09-19T12:00:00Z",
  "commit": "78821075",
  "suites": ["unit"],
  "specifications": { "cited": 63, "registered": 36 },
  "documents": [{
    "key": "oauth/rfc7636",
    "source": "RFC 7636",
    "title": "Proof Key for Code Exchange by OAuth Public Clients",
    "url": ["https://www.rfc-editor.org/rfc/rfc7636"],
    "inventory": { "count": 17, "sections": ["4"], "unheld": 7 },
    "dismissed": 8,
    "requirements": [{
      "id": "oauth/rfc7636/verifier-must-match-challenge",
      "level": "MUST",
      "text": "If the values are not equal, an error response indicating …",
      "url": ["https://www.rfc-editor.org/rfc/rfc7636#section-4.6"],
      "state": "satisfied",
      "lane": "",
      "entry": { "file": "internal/spec/registry/oauth/rfc7636.yaml", "line": 126 },
      "claimants": [{
        "test": "TestTokenExchangeWrongVerifier",
        "file": "internal/oauth/server_test.go",
        "line": 236,
        "observations": ["status=400 error=invalid_grant"]
      }],
      "mutants": { "written": 3, "killed": 3 }
    }]
  }]
}
```

`state` is one of `satisfied`, `deferred`, `gap`, `not-run`, `uncovered`, and
carries `reason` for the two that have one. The states are the report's
existing vocabulary; nothing new is invented for publication.

`schema` is there because the moment this is attested it becomes an interface.

## What has to be built to produce it

Four of these are joins over data that already exists. One is not.

1. **Line numbers in the loader.** `internal/spec` decodes YAML into structs
   and throws positions away. SARIF requires a location on every result, and
   the page wants to link a requirement to its entry. This means decoding
   through `yaml.Node` and recording file and line per requirement and per
   dismissal — the only part of this design that touches the registry's own
   parsing, and the part to do first because everything else waits on it.

2. **Claimant positions in the runtime report.** The static scan already
   carries `File` and `Line` per claim; the claims file carries the test name
   and its observations. The JSON wants both, so the report joins them on
   (id, test name). Both halves exist; this is bookkeeping.

3. **Mutant results as data.** `spec-gate mutants` prints a count to stderr and
   exits. Without a machine-readable result the page can only say how many
   mutants a requirement *has*, which is a claim about the catalog rather than
   about this commit. `--json` writing per-requirement kill results is what
   makes `"killed": 3` truthful.

4. **Extract counts as data.** `spec-extract` already computes the unheld count
   per document. `baseline.yaml` is the committed floor, not the live number;
   the report should carry the live one and fall back to the baseline when the
   tool did not run, saying which it used.

5. **`--format=json`** over the merged whole.

## The page's contract

This is the part that decides whether the artifact is evidence or marketing,
and it is a decision about content rather than code.

The page publishes, with no less prominence than the satisfied list:

- every deferred requirement and its reason — currently 30, of which 20 are
  MUSTs and 10 are OAuth's
- each document's `inventory.count` against what `spec-extract` reads out of
  the document, and the unheld residue
- how many requirements carry a mutant that is not merely a disabling one
- how many rest on exactly one test
- how many are `lane: e2e`, and that CI never runs that lane
- the observation ratio, whatever it is

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
| document above its extract baseline | yes | warning |
| `uncovered` | yes | error |
| `satisfied`, `not-run` | omitted | — |

`uncovered` is in the table for completeness and will never appear: the report
already exits non-zero on one, so CI fails before it uploads anything. The
honest description of this surface is *deferrals, gaps and transcription
drift* — around thirty-five results against limits of 25,000 per run.

Each requirement becomes a `reportingDescriptor`: `id` is the full identifier,
`shortDescription` the first sentence of the requirement text, `fullDescription`
the whole of it, `help.text` the reason or the mutant summary, and `helpUri` the
requirement's own URL. `partialFingerprints` is a digest of the full identifier
rather than of the location, so moving an entry within its file does not close
one alert and open another.

## CI

Three jobs, because the mutant run is minutes and the rest is seconds.

- **`test`** already sets `CETACEAN_SPEC_CLAIMS` and runs the report. It gains
  `--format=json` and uploads the result.
- **`spec-mutants`** gains `--json` and uploads its result.
- **`compliance`**, needing both, merges them, uploads the SARIF, and publishes
  the JSON as the artifact the website build consumes.

**The data file is not committed.** The registry's own design rules out a
committed coverage artifact, and a bot commit rewriting a generated file on
every push is the thing that rule is about. The consequence is that a local
`pnpm --filter website dev` has no compliance data, so the page must render an
honest "not built in this environment" rather than failing or, worse, showing a
stale copy as current.

## Deliberately not in this cut

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
makes the page worth reading is the mutant kills, the observations, and the
denominators — and of those, the denominators do the most work, because they are
the only part a reader can use to discount the rest.

**The observation is still self-reported.** A test chooses what to record, so a
test that asserts loosely records loosely. `Observed` moves the lie from "a test
passed" to "a test says it saw this", which is further than it sounds but is not
proof.

**`schema: 1` will be wrong eventually.** Attesting a shape freezes it. Adding a
field is safe; changing what `state` means is not, and there is nothing here
that would catch the second.
