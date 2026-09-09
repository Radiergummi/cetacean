# MCP catalog manifest: design

## Problem

`docs/mcp-tools.mdx` is the catalog an agent sees over the MCP server, and every
fact on it is typed by hand against a Go source that already holds it. The page
states, today:

- that there are "12 resources, 27 tools, 6 prompts, and 5 widgets" — four
  numbers, in one sentence, that no reader of a Go change will think to revisit
- an operations level on each of 27 tool cards, against `toolDef.tier`
  (`internal/mcp/tools.go:144`)
- an argument list on each of those cards, against the tool's input schema
- a six-row prompts table whose Level, Argument and Reads columns restate
  `promptTier`, the prompt's arguments and `promptDef.reads`
  (`internal/mcp/prompts.go:32`, `:269`)
- a five-row apps table, against `widgetCatalog` (`internal/mcp/ui.go:49`) and
  the `toolDef.widget` that names each view

None of it is checked. The page carried a second copy of the tool-to-level
mapping until 2026-09-09, when it was removed for exactly this reason; the
remaining copies are the ones that cannot be removed, because they are what the
page is for.

The catalog is correct as of this writing. That was established by running the
check this design proposes, by hand: all 27 tools have a card, no card names a
tool that does not exist, and every argument in every schema appears in its
card's argument list. The point is not that the page is wrong. It is that
nothing would say so if it were, and the argument lists in particular were
transcribed by hand — 27 of them, carrying defaults and enum values — in a
single sitting.

The repository already answers this class of problem one directory away. The
error reference is generated: `scripts/dump-errors` (40 lines plus a test)
writes `website/src/data/errors.json` from `sync-assets`, and `ErrorCode.astro`
is pure presentation over it. `pages.yml` watches the source the generator
reads. The MCP catalog is the same shape of problem and got the opposite
treatment.

What stops a straight copy of that answer is that most of this page is worth
writing by hand. The tool cards carry operational judgement a schema cannot
express — that a task read is the only way to reach a replica that has already
exited, that a service in a restart loop holds seconds of history, that missing
cAdvisor is reported rather than charted as zero. The Go descriptions cover
similar ground but are written for a model: imperative, packed, and tuned for
tool selection rather than for reading. Generating the page from them would
lose the writing, which is the page's reason to exist.

## Decision

Generate what has no editorial content; check what does.

### 1. `scripts/dump-mcp` emits a manifest

A `main.go` in the shape of `scripts/dump-errors`, run from `sync-assets` in
`website/package.json`, writing `website/src/data/mcp.json`. The output is
gitignored and rebuilt on every build, as `errors.json` is
(`website/.gitignore:14`), so it cannot go stale in the tree.

It constructs a server with no dependencies —
`New(cache.New(nil), Options{Config: DefaultMCPConfig(), GlobalOpsLevel: OpsImpactful})`,
the construction `newResourceTestServer` already uses
(`internal/mcp/resources_test.go:19`) — so no Docker, Prometheus, ACL or
network is involved. `GlobalOpsLevel` is the highest tier so every tool is
present regardless of what a deployment would register.

It emits, per tool: `name`, `tier`, `arguments` (each property's name, whether
it is required, and its enum values and default where the schema declares
them), `widget`, and the four behavioural hints. Per prompt: `name`, the tier
`promptTier` derives from `drives`, `arguments`, and `reads`. Per widget: the
`widgetCatalog` name, title and description, plus the tool whose `toolDef.widget`
names it. Plus the four counts.

Arguments come from the **built** schema rather than the Go source text. This
is load-bearing: `update_service` declares `value` with `mcplib.WithAny`
(`internal/mcp/tools_configuration.go:48`) where its siblings use `WithString`,
so any check that reads the source with a pattern reports a false positive on
it. A test beside the script, as `dump-errors` has, asserts the manifest is
non-empty and that every tool carries a tier.

### 2. Three parts of the page render from it

The intro's count sentence, the prompts table and the apps table. Each table
becomes a component taking the manifest for its mechanical columns and a
`notes` object, keyed by name, for its one prose column:

```mdx
<PromptTable notes={{
  diagnose_service: "Walks tasks, the failing task's logs, metrics, and recent
                     changes to find why a service is unhealthy",
}} />
```

The prose stays in the document, where it is written and reviewed. The Level,
Argument and Reads columns stop being typed. The check below requires the
`notes` key set to equal the manifest's, so a prompt added in Go fails the
build until someone writes its sentence — which is the outcome worth having:
the page cannot silently omit a prompt, and cannot describe one that is gone.

### 3. The tool cards stay hand-written, and are checked

`src/lib/mcp.ts` loads the manifest, reads `docs/mcp-tools.mdx` as text, and
throws on any of:

- a tool in the manifest with no `<McpTool>` card
- a card naming a tool the manifest does not have
- a card whose `level` differs from the tool's tier
- an argument in a tool's schema that does not appear as a backticked token in
  that card's `arguments` fact
- a `notes` key set, on either generated table, that differs from the manifest's

The build is static, so a throw fails `astro build` — the same mechanism as the
`assertRendered` guard in `src/pages/[...slug].md.ts`, which fails the build
when an MDX component has no Markdown fallback.

The argument rule runs in one direction only: every schema property must be
mentioned, but not every backticked token must be a property. The argument
facts deliberately mix names with values — ``` `view` (`network`, `placement`,
`drain-impact`) ``` — and a two-directional rule would flag every enum value on
the page. The consequence is that an argument removed from Go but left in the
docs is not caught. That is the weaker risk: a stale argument reads as an error
to anyone who calls the tool, where a missing one is invisible.

### 4. CI

`pages.yml` gains `internal/mcp/**` and `scripts/dump-mcp/**` to its `paths`.
This is broader than the `internal/api/errors.go` precedent because the
manifest draws on `tools_*.go`, `prompts.go`, `ui.go` and `resources.go` —
most of the package. Any MCP change will rebuild the docs site. That is correct
and noisier, and the noise is the price of the site never lagging the catalog.

## Non-goals

- **Generating the tool prose.** The cards are the page's value and stay
  hand-written. This design deliberately does not add a `docs:` field to
  `toolDef`.
- **Generating the resources section.** Its 12 entries read like data but
  several carry real explanation, and it is left alone by request. Only the
  resource *count* comes from the manifest.
- **Committing the manifest.** It is build output, like `errors.json`. A
  committed copy would need its own sync check, which is the problem this
  design exists to remove.
- **Checking prose against Go descriptions.** The two are written for different
  readers and are expected to differ.
- **A Go-side test that reads the docs.** The check lives where the manifest is
  already loaded, so there is one mechanism rather than two. The cost is that it
  fails in the site build rather than in `go test ./...`, which is the wrong
  half of CI for a Go author to notice — accepted because `pages.yml` will run
  on the same push.
