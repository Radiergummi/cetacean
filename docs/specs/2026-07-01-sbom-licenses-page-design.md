# Open-Source Licenses Page (SBOM-backed)

**Date:** 2026-07-01

## Summary

Add an in-app "Open-Source Licenses" page that lists every open-source project bundled into Cetacean — Go
modules and the frontend's npm dependencies — in a searchable, filterable grid. The data is a CycloneDX SBOM
generated at development time, committed to the repo (alongside the lockfiles it summarizes), embedded into the
binary via `//go:embed`, and served from two public meta endpoints. The React page fetches a projected JSON shape
and renders cards with name, version, license, description, and links. The page is reachable from the footer.

This mirrors the pattern already validated in the `big-spender` project, adapted to Cetacean's conventions.

## Motivation

Cetacean is GPLv3 and ships a single binary that embeds a large React frontend and links many Go modules. Users and
downstream distributors benefit from a transparent, self-served inventory of the open-source components inside the
binary — both as attribution and as a supply-chain artifact (a standard CycloneDX SBOM they can pull for their own
tooling). The lockfiles (`go.sum`, `frontend/package-lock.json`) already live in the repo; a committed SBOM is a
natural, human-facing extension of them.

## Resolved decisions

These were settled during brainstorming and are fixed for this spec:

1. **Scope: Go modules + frontend npm packages.** No OS/base-image component scan — the runtime image is
   `FROM scratch` (only a CA-cert file is copied in), so there are no OS packages to enumerate. This drops the
   `syft` dependency that `big-spender` used for its Debian base image.
2. **Visibility: public.** Both data endpoints live under the auth-exempt `/-/` meta prefix. Open-source
   attribution is non-sensitive, and this lets supply-chain tooling pull the raw CycloneDX SBOM without
   credentials. The rendered page itself rides the SPA (so under an auth mode it is viewable once logged in, like
   the rest of the UI), but the underlying data is public.
3. **Lifecycle: commit the real SBOM.** The generated CycloneDX file is committed to the repo (not gitignored).
   Freshness is guaranteed by a mandatory CI `make sbom-check` gate (regenerate + `git diff --exit-code`), and kept
   low-friction locally by an opt-in `make hooks` pre-commit hook. The Docker build embeds the committed file as-is
   and needs no SBOM tooling.

## SBOM generation

A shell script `scripts/build-sbom.sh` produces the committed artifact
`internal/api/sbom/sbom.cdx.json` (CycloneDX JSON):

1. **Go modules** — `cyclonedx-gomod mod -json -licenses -output <tmp>/sbom-go.cdx.json`.
   The `mod` subcommand (not `app`) avoids requiring a git-tagged version.
   Installed via `go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@<pinned>`.
2. **npm packages** — `cyclonedx-npm --omit dev --output-file <tmp>/sbom-npm.cdx.json`, run in `frontend/`
   (requires `node_modules` present). Installed via `npm install -g @cyclonedx/cyclonedx-npm@<pinned>` or run
   through `npx`.
3. **Merge** — `cyclonedx merge --hierarchical --name cetacean --version <version> --output-file …`, from
   `cyclonedx-cli`. In CI this binary is fetched from the CycloneDX GitHub releases (linux-x64/-musl), **not**
   Homebrew, so the step is portable on Linux runners.
4. **Determinism** — strip volatile fields so the committed file only changes when dependencies actually change:
   remove the random `serialNumber` and the `metadata.timestamp` (via `jq`). Without this the file would diff on
   every regeneration and defeat the `sbom-check` gate.

Tool versions are pinned in the script (and referenced from `tools.go` for the Go tool where practical) so
generation is reproducible.

## Lifecycle: commit + CI gate + git hook

- **Committed artifact.** `internal/api/sbom/sbom.cdx.json` is tracked. `//go:embed` therefore always has a real,
  populated file; a bare `go build` on a fresh checkout compiles with no extra steps.
- **`make sbom`** regenerates the committed file in place.
- **`make sbom-check`** (CI, mandatory) regenerates and runs `git diff --exit-code -- internal/api/sbom/sbom.cdx.json`,
  failing if the committed file is stale. This is the actual freshness guarantee — it is not bypassable, and it
  catches dependency bumps (including Dependabot PRs) that didn't regenerate the SBOM.
- **Opt-in pre-commit hook** (`.githooks/pre-commit`, installed by `make hooks` via `git config core.hooksPath
  .githooks`). The hook regenerates and re-stages the SBOM **only when a dependency manifest is staged**
  (`go.mod`, `go.sum`, `frontend/package.json`, `frontend/package-lock.json`); otherwise it no-ops, so ordinary
  commits are not slowed. The hook is a convenience that keeps developers ahead of the CI gate; it is explicitly
  not the enforcement mechanism (hooks are local, uncloned, and `--no-verify`-bypassable). If the SBOM toolchain
  is missing, the hook warns and skips rather than blocking the commit.
- **Docker build unchanged.** The multi-stage build embeds the committed `sbom.cdx.json` via the existing
  `COPY . .`; no cyclonedx tooling is added to any Docker stage.

## Backend

### Package `internal/api/sbom/`

Self-contained, following the reference layout:

- **`embed.go`** — `//go:embed sbom.cdx.json var raw []byte`; exposes `Raw() []byte` and `ETag() string`
  (a cached SHA-256 of `raw`, quoted for HTTP).
- **`project.go`** — `Project() Document` parses the embedded CycloneDX bytes once and caches the flattened
  projection plus its ETag. Mapping per component:
  - `name`, `version`, `description` from the component fields.
  - `ecosystem` derived from the purl scheme: `pkg:golang` → `"go"`, `pkg:npm` → `"npm"`, anything else →
    `"other"`.
  - `licenses` from CycloneDX `licenses[]`, handling both `license{id|name,url}` and `expression` entries.
  - `homepage` from an `externalReferences` entry of type `website`; `repository` from type `vcs`.
  - `generatedAt` sourced from the binary's build date (`internal/version.Date`) at projection time, since the
    SBOM's own timestamp is stripped for determinism.

  Note: the projected field is named `ecosystem` (not `group`, as in `big-spender`) to avoid confusion with
  CycloneDX's own component `group`/namespace field. This is the single intentional shape deviation from the
  reference.

### Data shape (projected JSON)

```jsonc
{
  "generatedAt": "2026-07-01T00:00:00Z",
  "components": [
    {
      "name": "github.com/mark3labs/mcp-go",
      "version": "v0.55.1",
      "description": "…",
      "ecosystem": "go",
      "licenses": [{ "id": "MIT", "name": "", "url": "https://…" }],
      "homepage": "https://…",
      "repository": "https://github.com/mark3labs/mcp-go"
    }
  ]
}
```

Go types (in `internal/api/sbom`):

```go
type Document struct {
    GeneratedAt string      `json:"generatedAt,omitempty"`
    Components  []Component `json:"components"`
}
type Component struct {
    Name        string    `json:"name"`
    Version     string    `json:"version,omitempty"`
    Description string    `json:"description,omitempty"`
    Ecosystem   string    `json:"ecosystem"`            // "go" | "npm" | "other"
    Licenses    []License `json:"licenses"`
    Homepage    string    `json:"homepage,omitempty"`
    Repository  string    `json:"repository,omitempty"`
}
type License struct {
    ID   string `json:"id,omitempty"`   // SPDX id when known
    Name string `json:"name,omitempty"`
    URL  string `json:"url,omitempty"`
}
```

### Handlers and routes

`internal/api/licenses.go` — two handlers, both with `ETag`/`If-None-Match` → 304 and `Cache-Control`:

- `GET /-/sbom.cdx.json` → raw embedded CycloneDX bytes.
  `Content-Type: application/vnd.cyclonedx+json; version=1.6`, `Cache-Control: public, max-age=300, must-revalidate`.
- `GET /-/licenses` → projected `Document` JSON.
  `Content-Type: application/json`, `Cache-Control: public, max-age=300`.

Registered in `internal/api/router.go` next to the other `/-/` meta routes. Because they sit under the `/-/`
prefix, they are already auth-exempt and outside content negotiation and discovery-link middleware — no change to
`auth/middleware.go isExempt` is required.

Unlike the OpenAPI spec (which is embedded at the repo root in `main.go` and passed through `api.RouterConfig`),
the SBOM lives in its own package `internal/api/sbom` that owns the `//go:embed`. `internal/api` imports it
directly, so the handlers call `sbom.Raw()` / `sbom.ETag()` / `sbom.Project()` with no bytes threaded through
`main.go` or `RouterConfig`. `main.go` is unchanged.

## Frontend

- **Route** `/licenses` added to `frontend/src/App.tsx`; lazy-loaded `pages/Licenses.tsx`.
- **API** `api.licenses()` in `frontend/src/api/client.ts` fetching `/-/licenses`; types `LicensesResponse`,
  `LicenseComponent`, `LicenseEntry` in `frontend/src/api/types.ts`.
- **Page** — responsive card grid built from existing Tailwind v4 / shadcn primitives:
  - A name search input (case-insensitive substring over component name).
  - An ecosystem filter (All / Go / npm — and Other only if present) showing per-group counts.
  - Each card: component name linking to `homepage` when present, a version badge, an ecosystem badge, license
    badge(s) (SPDX id or name), a line-clamped description, and a repository link.
  - Sensible loading and error states consistent with other pages (`FetchError`, skeletons).
- **Footer** — add a "Licenses" link to the existing nav in `frontend/src/components/Footer.tsx`.

## Build / CI wiring

- **Makefile** — add `sbom` (runs `scripts/build-sbom.sh`) and `sbom-check` (regenerate + `git diff --exit-code`)
  targets, and a `hooks` target (`git config core.hooksPath .githooks`). `make check` gains `sbom-check`.
- **`.githooks/pre-commit`** — committed hook script as described in the lifecycle section.
- **GitHub Actions** — a CI step runs `make sbom-check`, installing cyclonedx-gomod (go install), cyclonedx-npm
  (npm), and the cyclonedx-cli release binary, and running `npm ci` in `frontend/` first so cyclonedx-npm sees
  `node_modules`.
- **Dockerfile** — unchanged; the committed `sbom.cdx.json` is embedded via the existing source copy.

## Testing

- **Go** — `project_test.go` drives `Project()` against a small committed CycloneDX fixture
  (`testdata/example.cdx.json`) asserting: ecosystem derivation from purl, license id/name/url extraction
  (including an `expression` entry), and homepage/repository mapping from external references. `licenses_test.go`
  exercises the two handlers: content types, ETag round-trip → 304, and body correctness.
- **Frontend** — a vitest test for `Licenses.tsx` covering name search and ecosystem filtering (correct subset and
  counts).

## Non-goals

- No OS/base-image component scanning (runtime is `scratch`).
- No dev-dependency inventory (npm generation uses `--omit dev`).
- No license-policy enforcement or "forbidden license" gating — this feature is inventory/attribution only.
- No per-request SBOM generation — projection happens once at init.

## File-by-file change list

New:
- `scripts/build-sbom.sh`
- `internal/api/sbom/embed.go`, `project.go`, `sbom.cdx.json` (committed artifact),
  `testdata/example.cdx.json`, `project_test.go`
- `internal/api/licenses.go`, `licenses_test.go`
- `.githooks/pre-commit`
- `frontend/src/pages/Licenses.tsx` (+ test)

Modified:
- `internal/api/router.go` (register the two `/-/` routes; import `internal/api/sbom`)
- `frontend/src/App.tsx` (route), `frontend/src/api/client.ts` (method),
  `frontend/src/api/types.ts` (types), `frontend/src/components/Footer.tsx` (link)
- `Makefile` (`sbom`, `sbom-check`, `hooks`, `check`)
- `.github/workflows/*` (CI `sbom-check` step)
- `CHANGELOG.md`
