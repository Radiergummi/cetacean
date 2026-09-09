# Error reference and target-aware link icons: design

## Problem

The website carries three references — the API explorer over `api/openapi.yaml`,
the MCP tool catalog in `docs/mcp-tools.md`, and the schema reference at
`/api/schema` — and no error reference. The dashboard has one: `GET /api/errors`
lists all 100 codes with their title, status, description and suggestion, and
`GET /api/errors/{code}` returns one, both served from `errorRegistry`
(`internal/api/errors.go:40`). A reader following a `type` URI out of a problem
response — `"type": "/api/errors/SVC001"` — reaches that JSON only if they are
pointed at a running Cetacean. On the public site the URI resolves to nothing.

What `docs/api.md` offers instead is a hand-written table of the twenty domain
prefixes and a five-row table of common errors. Neither names a code's title,
status, description or suggestion, and the prefix list is a third copy of a fact
the Go source already states — after the comment block at
`internal/api/errors.go:19` and the registry itself.

**That third copy is already wrong.** The comment block lists nineteen prefixes;
the registry holds twenty. `ACL` — the prefix behind `ACL001` and `ACL002`, the
two codes `docs/api.md` calls out by name as common — is missing from it. A
hand-written error reference on the website would be a fourth copy, and would
drift the same way.

The second problem is smaller and unrelated in mechanism. Links in the docs are
undifferentiated: `[operations level][operations-level]`, `[ACL][authorization]`
and `[API explorer][api-explorer]` render identically, so a reader cannot tell
from the link whether it goes to a setting, a guide, or a reference entry until
they follow it. The site has five link destinations that are references rather
than prose, and they are worth distinguishing at a glance.

## Decision

Two independent changes that share only a destination.

### 1. The catalog becomes build input for the site

`internal/api` gains two exported values and no new behaviour:

```go
// ErrorDefs returns every well-known error, ordered by code.
func ErrorDefs() []ErrorDef

// ErrorDomains names each code prefix, in the order the reference presents them.
var ErrorDomains = []ErrorDomain{
    {Prefix: "API", Label: "Protocol and content negotiation"},
    ...
}
```

`ErrorDefs` is the body of `HandleErrorIndex` (`internal/api/errors.go:609`)
lifted out, so the handler and the website build read one function rather than
two loops that can disagree. `ErrorDomains` is a slice rather than a map because
the order carries meaning — protocol, then auth, then the cross-cutting
subsystems, then one entry per resource type, which is how `docs/api.md`
presents it and not how the prefixes sort. The comment block at
`internal/api/errors.go:19` is deleted: it is the copy that already went stale,
and `ErrorDomains` says the same thing where a compiler and a test can see it.

A test in `internal/api/errors_test.go` asserts every prefix present in
`errorRegistry` appears in `ErrorDomains` and vice versa, so adding a code
under a new prefix fails rather than silently producing an ungrouped entry on
the site.

`scripts/dump-errors/main.go` is a `package main` that JSON-encodes
`{"domains": ErrorDomains, "errors": ErrorDefs()}` to stdout. It exists because
`internal/api` cannot be read by anything outside the module and the website
build is outside the module.

`website/package.json`'s `sync-assets` — which already copies `openapi.yaml`
and the Scalar bundle into place before every `dev` and `build` — gains a line
writing `src/data/errors.json`. `website/src/lib/errors.ts` imports that file
and re-exports it typed, matching the shape `src/lib/schema.ts` exports so the
two reference pages consume their data the same way. The generated file is
listed in `website/.gitignore` beside the other sync-assets output.

This makes Go a build dependency of the website, which it was not before. The
GitHub Pages job (`.github/workflows/pages.yml`) gains an `actions/setup-go`
step. `ci.yml`'s `lint-website` job runs `oxlint` and `oxfmt` without building
and needs no change.

### 2. `/api/errors` on the website

`website/src/pages/api/errors.astro`, built the way `api/schema.astro` is: a
`DocsLayout`, one `<h2>` per domain, and a `ReferenceEntry` per code with
`id={code}` — so `api/errors#SVC001` resolves, giving the `type` URI a problem
response carries (`/api/errors/SVC001`, readable only against a running
Cetacean) a public counterpart on the site. Each entry shows the title as its
heading, the HTTP status as a `Badge`, and the description and suggestion as the
body and a fact row.

**The table of contents lists domains only.** Every code getting an entry would
make a 120-line contents column for a page whose entries are already grouped and
alphabetical within a group; the twenty domains are the only navigation a reader
of this page needs. This deliberately differs from `/api/schema`, which lists
every type and property — that page has no grouping to navigate by instead.

The page joins `sidebarGroups` in `src/lib/navigation.ts` under **Reference**,
after Schema Reference. Pagefind indexes it as it does every other page, so
searching a code from the site header finds it without the page carrying a
filter of its own.

`docs/api.md` loses its prefix table — the copy that motivated this — in favour
of a link to the new page, keeps the `GET /api/errors` endpoint sentence, which
documents the API rather than restating the catalog, and links the codes it
names in prose and in the common-errors table.

### 3. Link icons, in CSS

Links are authored, not detected. No remark or rehype plugin, no string
matching over prose: a rule per destination in `global.css`, keyed on `href`.

```css
.prose a[href*="api/errors#"]::before { --link-icon: url("data:image/svg+xml,…"); }
```

The icon is drawn by a shared `::before` whose `mask-image` is `--link-icon` and
whose `background` is `currentColor`, so it takes the link's colour and needs no
dark-mode variant — the same technique and the same Heroicons source the
callouts in `astro.config.ts` already use.

| Destination | Selector | Icon |
|---|---|---|
| Error code | `[href*="api/errors#"]` | exclamation-circle |
| Setting | `[href*="configuration#"]` | adjustments-horizontal |
| API reference | `[href*="api/explorer"]` | code-bracket-square |
| Schema type | `[href*="api/schema#"]` | cube |
| MCP tool | `[href*="mcp-tools#"]` | wrench-screwdriver |
| External | `[href^="http"]` | arrow-top-right-on-square |

Each selector uses `*=` rather than `^=` because doc links are relative:
`configuration#auth.mode` as authored in `docs/authentication.md`,
`/configuration#auth.mode` if a page ever writes it absolutely. Both forms
contain the same substring, and no other href on the site does.

**Icon only, no colour.** Every link keeps `--link`. Five hues in a paragraph
reads as decoration rather than meaning, and a distinction carried by colour
alone is not available to every reader; the icon carries it unambiguously and
survives both themes for free.

Scoping is `.prose`, so the sidebar, header and card chrome are untouched.

One consequence in the docs: `docs/configuration.mdx` writes its own settings as
same-page links (`[mcp.enabled]: #mcp.enabled`, 23 of them), which no
`configuration#` selector can match. Those definitions are normalised to the
`configuration#mcp.enabled` form every other doc already uses — the same
destination, and a uniform rule instead of a page-scoped exception in the
stylesheet. `docs/mcp-tools.md`'s one same-page definition (`[icons]: #icons`)
points at a section rather than a tool and is left alone.

## Non-goals

**No auto-linking.** An earlier draft rewrote inline code spans matching a
catalog entry into links. It is not worth the false positives: `Service`,
`Node`, `find` and `describe` are the schema types and MCP tools *and* the
ordinary words the docs use constantly, and every rule that catches the
reference sense catches the prose sense. Links stay authored.

**No machine-readable copy on the website.** The generated JSON is build input
under `src/data/`, not an asset under `public/`. A running Cetacean already
serves `GET /api/errors`; a second copy on a static site would be one more thing
to keep in step.

**No error detail pages.** One page with an anchor per code, not 100 routes.
The entries are four short fields; a page each would be navigation without
content.

**The schema reference stays hand-written.** `src/lib/schema.ts` has the same
staleness exposure this change removes for errors, but the JSON-LD vocabulary
has no single Go value to generate it from — it is spread across `@context` in
`internal/api/context.go` and the response structs. That is a separate design.
