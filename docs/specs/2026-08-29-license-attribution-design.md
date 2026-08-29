# License Attribution: Full Texts, Notices, and Filtering

**Date:** 2026-08-29

## Summary

Extend the SBOM pipeline to harvest each dependency's actual license text and `NOTICE` file, embed
them alongside the existing CycloneDX document, and surface them on the licenses page. Adds a license
filter to that page, a per-component license-text view, and a generated `THIRD_PARTY_LICENSES` that
replaces today's hand-curated file.

This builds directly on `2026-07-01-sbom-licenses-page-design.md`, which established the SBOM
generation, embedding, and page. That spec deliberately stopped at an inventory; this one closes the
gap between an inventory and an attribution document.

## Motivation

The licenses page currently lists SPDX identifiers. That is a useful supply-chain inventory, but it
does not discharge Cetacean's obligations as a distributor.

Every permissive license in the dependency tree requires the copyright notice **and the license text**
to travel with the distribution:

| License | Count | Obligation |
|---|---:|---|
| MIT | 65 | Reproduce copyright notice + permission notice in all copies |
| Apache-2.0 | 32 | Reproduce license; propagate `NOTICE` contents (§4(d)); state modifications (§4(b)) |
| BSD-3-Clause | 24 | Reproduce copyright + conditions + disclaimer in binary distributions |
| ISC | 9 | Reproduce copyright + permission notice |
| BSD-2-Clause | 3 | Reproduce copyright + conditions + disclaimer |
| 0BSD | 1 | None (notice explicitly not required) |
| CC-BY-SA-4.0 | 1 | Attribution + license notice (`opencontainers/go-digest`, spec text) |
| OFL-1.1 | 1 | Bundle copyright + license; Reserved Font Name applies if modified (`@fontsource-variable/geist`) |
| EPL-2.0 OR GPL-3.0-or-later | 1 | Dual-licensed; Cetacean elects GPL-3.0-or-later (`elkjs`) |

An SPDX identifier is a *label for* a license text; it is not the text, and it never carries the
per-package copyright line. Two MIT dependencies have identical identifiers and different copyright
holders, and it is the copyright line that attribution is about.

Two concrete gaps today:

1. **`THIRD_PARTY_LICENSES` covers only Lucide and Feather.** It states outright that it "does not
   attempt to enumerate every transitive build-time dependency."
2. **No `NOTICE` file is propagated.** Eight dependencies ship one — `docker/docker`,
   `CycloneDX/cyclonedx-go`, `coreos/go-oidc/v3`, `yaml.v3`, and four `prometheus/*` modules. Apache-2.0
   §4(d) makes carrying these mandatory, and none of them currently reach a user.

The user-facing ask that prompted this work — a license filter on the page — is folded in here because
it touches the same data and the same component.

## Feasibility (measured, not assumed)

Probed against the committed SBOM on 2026-08-29:

- **137 of 137 components have a discoverable license file.** No fallback path is needed for the
  current tree; a missing file is treated as a generation failure rather than a silent gap.
- **509,845 bytes of license text**, falling to **307,091 bytes across 85 distinct texts** when
  deduplicated by content hash. Apache-2.0's boilerplate is byte-identical across its 32 users; MIT
  texts differ precisely because they carry different copyright lines.
- **8 components ship a `NOTICE` file.**

310KB of embedded text against a binary already tens of megabytes is not a size concern for the
binary. It *is* a concern for the `/-/licenses` JSON payload, which the page fetches on load — hence
the split below.

## Design

### Harvesting (`scripts/build-sbom.sh`)

After the existing merge step, a new Go generator (`scripts/licensetexts/`, run via `go run`) walks the
merged SBOM and resolves each component to a source directory:

- **Go modules** — `$(go env GOMODCACHE)/<escaped-module>@<version>`, where `escaped-module` applies
  the module cache's uppercase escaping (`X` → `!x`).
- **npm packages** — `frontend/node_modules/<group>/<name>`.

In each directory it looks for `LICENSE*`, `LICENCE*`, `COPYING*` (first match, sorted, for the license
text) and `NOTICE*` (for the notice). A component that resolves to neither a directory nor a license
file **fails the build** with the component named — silence here is how attribution rots.

A Go generator rather than shell: the path escaping, the dedup hashing, and the deterministic JSON
output are all things `jq` and `bash` do badly, and it can reuse `internal/api/sbom`'s types.

### Embedded artifact

A second committed file, `internal/api/sbom/licensetexts.json`, embedded next to `sbom.cdx.json`:

```json
{
  "texts": { "<sha256-prefix>": "<full license text>" },
  "components": {
    "npm:@floating-ui/core@1.7.4": { "license": "<sha256-prefix>", "notice": "<sha256-prefix>" }
  }
}
```

Keys match the projection's existing `ecosystem:name@version` identity, which the scoped-name fix made
unambiguous. Texts are pooled and content-addressed, so the 32 Apache-2.0 copies cost one.

Splitting this from `sbom.cdx.json` keeps the CycloneDX document a valid, unmodified CycloneDX
document for external tooling.

### API

| Endpoint | Change |
|---|---|
| `GET /-/licenses` | Each **component** gains `textId` and, when present, `noticeId`. Payload stays small — identifiers only. The ids sit on the component rather than on each `licenses[]` entry because a package ships one license file however many identifiers describe it: `elkjs` declares `EPL-2.0 OR GPL-3.0-or-later` and has a single `LICENSE`. |
| `GET /-/licenses/texts/{id}` | **New.** `text/plain; charset=utf-8`, immutable cache headers (content-addressed), ETag. 404 on unknown id. |
| `GET /-/notices` | **New.** The full generated attribution document as `text/plain`. Byte-identical to `THIRD_PARTY_LICENSES`. |

All three stay under the auth-exempt `/-/` prefix, consistent with the existing decision that
attribution data is public.

### Generated `THIRD_PARTY_LICENSES`

`make sbom` regenerates it: a header explaining scope, then one section per component (name, version,
homepage, SPDX id, license text, and `NOTICE` contents when present), ordered by ecosystem then name.

The current file's hand-written Lucide/Feather section is **preserved as a curated preamble**, because
it documents something the SBOM cannot see: icon SVGs whose path data was copied into
`frontend/public/assets/mcp-icons/`, which is a derivation rather than a dependency. The generator
concatenates `THIRD_PARTY_LICENSES.preamble` (tracked, hand-edited) ahead of the generated body.

Three components need a curated note in that preamble, since a bare license text understates them:

- **`elkjs`** — record that Cetacean elects GPL-3.0-or-later from its `EPL-2.0 OR GPL-3.0-or-later`
  offer. A dual license is a choice, and an unexercised choice is ambiguous downstream.
- **`@fontsource-variable/geist`** — note the font is redistributed unmodified under OFL-1.1, so the
  Reserved Font Name provision is not triggered.
- **`opencontainers/go-digest`** — note that CC-BY-SA-4.0 covers its specification text, that the code
  is Apache-2.0, and that Cetacean redistributes both unmodified.

Apache-2.0 §4(b) — stating modifications — needs no action: no dependency is vendored or patched.

### Frontend

`pages/Licenses.tsx` gains, beside the existing ecosystem buttons:

- **A license filter.** A `Select` listing every distinct SPDX identifier present, with counts,
  defaulting to "All licenses". Combines with the existing ecosystem filter and search. A dropdown
  rather than more buttons: there are 9 licenses today and the set grows with the tree, where the
  ecosystem set is fixed at three.
- **A license-text view.** The license badge on each card becomes a button opening a `Dialog` that
  fetches `/-/licenses/texts/{textId}` on open (never on page load) and renders it in a scrollable
  monospace block. When a `noticeId` is present the dialog shows the `NOTICE` in a second, labelled
  section — the distinction matters legally, so it is not concatenated.
- **A "Download all notices" link** in the page header, pointing at `/-/notices`.

### Lifecycle

`make sbom-check` extends to regenerate and diff `licensetexts.json` alongside `sbom.cdx.json`, so a
dependency bump that changes a license fails CI the same way a stale SBOM does. The pre-commit hook
picks it up unchanged, since it already triggers on dependency-manifest changes.

Generation requires a populated `frontend/node_modules` and Go module cache — already true of
`make sbom` today, which needs `frontend/dist` and runs `cyclonedx-npm` against `node_modules`.

## Testing

- **Generator** — golden-file test over a small fixture tree: dedup collapses identical texts, the
  escaped Go module path resolves, a missing license file fails loudly, output is byte-stable across
  runs.
- **`internal/api/sbom`** — projection carries `textId`/`noticeId`; every id referenced by a component
  resolves in the pool (a consistency test over the *real* committed artifact, which catches a
  half-regenerated pair).
- **Handlers** — text endpoint returns `text/plain` and an ETag, 404s an unknown id, and 304s on
  `If-None-Match`; `/-/notices` matches the committed `THIRD_PARTY_LICENSES` byte for byte.
- **Page** — license filter narrows the grid and composes with ecosystem + search; the dialog fetches
  only on open; a component with a `NOTICE` renders both sections.

## Out of scope

- Scanning license texts to *verify* the declared SPDX identifier. The generators' detection is trusted,
  as it already is for the identifiers on the page today.
- Copyleft-obligation tooling or policy gates (e.g. failing CI on a newly introduced GPL dependency).
  Worth doing; a separate concern from attribution.
- The website's own dependency tree. `website/` is not distributed in the binary.

## Resolved decisions

**Texts are fetched lazily from `/-/licenses/texts/{id}`, not embedded in the `/-/licenses` payload.**
The alternative — inlining all 307KB — would be one fewer endpoint and one fewer round trip, but it
grows a page-load fetch roughly twentyfold to ship text most visitors never open. Because the ids are
content hashes, the responses are immutably cacheable, which takes the sting out of the extra request.
Reversible later if the endpoint proves not to earn its keep.
