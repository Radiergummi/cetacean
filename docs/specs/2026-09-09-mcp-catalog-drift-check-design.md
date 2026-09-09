# MCP catalog drift check: design

## Problem

`docs/mcp-tools.mdx` is the catalog an agent sees over the MCP server, and every
fact on it is typed by hand against a Go source that already holds it: an
operations level on each of 27 tool cards, against `toolDef.tier`
(`internal/mcp/tools.go:144`); an argument list on each card, against the tool's
input schema; the value sets those arguments accept; a six-row prompts table
restating `promptTier`, the prompt's arguments and `promptDef.reads`
(`internal/mcp/prompts.go:32`, `:269`); a five-row apps table; and four counts
in one sentence of the intro.

Nothing checks any of it. The page carried a second copy of the tool-to-level
mapping until 2026-09-09, when it was removed for exactly this reason; the
remaining copies are the ones that cannot be removed, because they are what the
page is for.

The catalog is correct as of this writing — established by running the rules
below by hand. The point is not that the page is wrong. It is that nothing would
say so if it were.

### Which direction the drift actually comes from

Measured over the repository's 1421 commits: **8** touched the MCP catalog files
(`tools*.go`, `prompts.go`, `ui.go`, `resources.go`), and **178** touched
`docs/`. The realistic failure is therefore not "someone changes Go and forgets
the page" but its opposite — a prose rewrite that drops an argument from a fact,
renames a card, or retypes a level. The 27 argument lists on this page were
themselves transcribed by hand in a single sitting, defaults and enum values
included.

That matters for where the check lives. `ci.yml` has no `paths:` filter, so
`go test ./...` runs on every pull request including documentation-only ones —
which is exactly the pull request where this class of mistake arrives.

### The one Go-side change worth planning for

`update_service` takes a `section` argument whose schema enum is
`mcplib.Enum(updateServiceSections...)`, and `updateServiceSections` is
`slices.Sorted(maps.Keys(serviceSectionWriters))` (`internal/mcp/updates.go:241`).
Adding an eleventh entry to that map is a one-key Go change that grows the enum,
invalidates a ten-row table in the card body, and falsifies the sentence
"`section` takes one of ten names". CLAUDE.md describes that fold as the thing
which absorbed eleven separate tools, so it is designed to grow this way.

## Decision

One test, `internal/mcp/docs_test.go`, in `package mcp`. It reads
`../../docs/mcp-tools.mdx` and compares it against the live catalog. Nothing is
generated and nothing new is wired.

Being in `package mcp` is what makes this cheap: `toolCatalog()`, `toolDef.tier`,
`promptCatalog()`, `promptTier` and `widgetCatalog` are all unexported, and a
test inside the package reaches them without adding a single exported symbol.

### The rules

1. **Card set.** Every tool in `toolCatalog()` has an `<McpTool>` card, and every
   card names a tool that exists.
2. **Level.** Each card's `level={n}` equals its `toolDef.tier`.
3. **Argument names.** Every property of a tool's input schema appears as a
   backticked token in that card's `arguments` fact.
4. **Enum values.** Every value in a schema enum appears as a backticked token
   somewhere in that card.
5. **Prompts and apps.** The prompts table's rows match `promptCatalog()` — name,
   `promptTier`, arguments and `reads` — and the apps table's rows match the
   widget directories under `frontend/src/widgets/`.
6. **Counts.** The intro's "12 resources, 27 tools, 6 prompts, and 5 widgets"
   matches the four catalogs.

### Why rules 3 and 4 have different scopes

Rule 3 reads the `arguments` fact only, with parenthesised groups removed. Rule 4
reads the whole card.

The narrow scope on rule 3 is load-bearing in both directions. Scanning the whole
card would be fail-quiet: `update_service`'s body backticks `env`, `labels`,
`resources`, `ports` and six more in its section table, so a property named after
any of them would pass without being documented as an argument. Dropping the
parenthesis exclusion would be fail-quiet too: `get_metrics` backticks `service`,
`node` and `cluster` as values of `target`, so adding a `service` argument — the
obvious ergonomic addition — would satisfy the rule without appearing anywhere.
The same holds for `get_topology` (`network`, `placement`), `update_node` and
`find`.

Rule 4 is wide because a value set is legitimately documented outside the
arguments fact: `update_service`'s ten sections are a table in the card body, not
a list in its fact, and that is the right place for them.

Both were run by hand over the current page. **Rule 3 passes on all 27 cards** at
this scope. **Rule 4 fails on two**: `create_secret` and `create_config` name
`encoding` without naming `utf8` or `base64`. That is real drift, found by the
rule on its first run, and the cards should be corrected as part of implementing
this.

### What it deliberately does not check

- **Defaults.** The page states `tail` (100), `limit` (200), `timeout` (60s,
  capped at 300) and `top` (5, capped at 10). None is a schema default —
  `grep -c "mcplib.Default"` across the four tool files is 0 — because they are
  handler constants. Checking them means naming each constant in the test, which
  is worth doing only if one of them ever moves.
- **An argument deleted in Go but left in the docs.** Rules 3 and 4 run
  schema→doc only, which is what gives them no false positives. The reverse
  direction would flag every enum value and every tool named in prose. The
  residual risk is mild: a stale argument errors for anyone who calls the tool,
  where a missing one is invisible.
- **The resources section.** Its 12 entries carry explanation rather than data.
  Only the resource *count* is checked, by rule 6.
- **Prose against the Go descriptions.** The two are written for different
  readers — one for a model choosing a tool, one for a person reading a
  reference — and are expected to differ.

## Alternative considered: a generated manifest

The first version of this design proposed `scripts/dump-mcp` writing
`website/src/data/mcp.json`, with the prompts and apps tables and the counts
rendered from it and the tool cards checked against it — the `dump-errors`
pattern, applied partially. It was rejected after review, on four findings:

1. **It does not compile.** `dump-errors` works because `api.ErrorDomains` and
   `api.ErrorDefs()` are exported. Everything the manifest needs from
   `internal/mcp` is unexported, so the script would have required a new exported
   `Manifest` surface on a domain package, existing solely for the docs site.
2. **It would have documented the wrong widget registry.** The manifest could
   only read `widgetCatalog`, which `internal/mcp/ui.go:47` and CLAUDE.md both
   state is presentation copy rather than the registry — widgets are discovered
   from `frontend/dist-widgets`, and a standalone script has no embed. A widget
   deleted with its copy left behind would have gone unnoticed.
3. **It reintroduced the duplication it existed to remove.** `assertRendered` in
   `website/src/pages/[...slug].md.ts` throws on any unhandled MDX component, so
   `<PromptTable>` would have needed a case in the raw-Markdown route that
   imported the manifest and rebuilt the table — a second implementation of the
   same table, able to disagree with the first.
4. **It generated the data least likely to drift.** The prompts and apps tables
   are 6 and 5 rows whose columns have effectively never changed, and generating
   them would also have cost the `{/* cards */}` treatment those two tables get:
   the card layout and the per-row anchors from `remarkCardTables`.

The check above catches everything that design would have caught, plus the enum
drift it missed, for one file and no build wiring.

## Non-goals

- **A manifest, in any form.** No JSON artifact, no `sync-assets` line, no
  `.gitignore` entry, no `pages.yml` paths.
- **New exported API on `internal/mcp`.** The test needs none.
- **Generating any part of the page.** Left open: if the prompts or apps tables
  ever start changing, generating them at mdast level — a remark plugin
  expanding a marker into a `table` node — would preserve `remarkCardTables`,
  the anchors and the raw route, and is the shape to reach for. Nothing here
  forecloses it.
- **Moving docs prose into Go.** The cards carry judgement a schema cannot
  express, and the Go descriptions covering similar ground are written for a
  model rather than a reader.
