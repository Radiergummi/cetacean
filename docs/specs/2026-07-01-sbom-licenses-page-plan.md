# Open-Source Licenses Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an in-app, searchable "Open-Source Licenses" page backed by a committed CycloneDX SBOM (Go modules + frontend npm), embedded in the binary and served from two public `/-/` endpoints.

**Architecture:** A build-time shell script generates a merged CycloneDX SBOM committed at `internal/api/sbom/sbom.cdx.json`. A dedicated `internal/api/sbom` package `//go:embed`s it, and at init projects the CycloneDX into a flat licenses `Document`. Two free handlers in `internal/api` serve the raw CycloneDX and the projected JSON under the auth-exempt `/-/` prefix. A React page fetches the projected JSON and renders a filterable/searchable card grid, linked from the footer. Freshness is guaranteed by a CI `make sbom-check` gate and kept low-friction by an opt-in pre-commit hook.

**Tech Stack:** Go 1.26, `cyclonedx-gomod` (Go tool dependency) + `cyclonedx-npm` (frontend devDependency) + `jq` (system), React 19 + TypeScript + Vite + Tailwind v4 + shadcn/ui, vitest.

## Global Constraints

- Go module path: `github.com/radiergummi/cetacean`.
- **Toolchain as declared dependencies:** `cyclonedx-gomod` is a Go `tool` directive in `go.mod` (invoked `go tool cyclonedx-gomod`); `cyclonedx-npm` is a `frontend` devDependency (invoked `npx --no-install cyclonedx-npm`). No ad-hoc `go install` / `npm -g`. `jq` is the only system prerequisite (used for the merge + strip; preinstalled on GitHub `ubuntu-latest`). There is no `cyclonedx-cli` dependency — the merge is done with `jq`.
- **Go generator uses `app`, not `mod`:** invoke `cyclonedx-gomod app -main . -json -licenses` so the SBOM reflects only what the `cetacean` binary actually imports. This excludes the `cyclonedx-gomod` tool's own dependency tree (which the `tool` directive adds to `go.mod`), keeping the licenses page accurate.
- **Scope:** Go modules + frontend npm production deps only (`--omit dev`). No OS/base-image scan.
- **Visibility:** data endpoints are public, served under the auth-exempt `/-/` prefix (no `isExempt` change).
- **Determinism:** the committed SBOM must have `serialNumber` and `metadata.timestamp` stripped (via `jq`) so it only diffs on real dependency changes.
- **Lifecycle:** the SBOM file is committed (tracked); CI `make sbom-check` is the freshness gate; the pre-commit hook is opt-in and must no-op when no dependency manifest is staged or when tooling is absent.
- **Projected field naming:** the ecosystem field is `ecosystem` (values `"go"`, `"npm"`, `"other"`) — deliberately not `group`, to avoid clashing with CycloneDX's component `group`/namespace.
- **Frontend style (CLAUDE.md):** no abbreviations in identifiers; always brace single-statement `if` bodies; blank lines around logical blocks; destructuring in callbacks; camelCase module-level constants.
- **CHANGELOG.md** must gain a concise, user-facing entry under `[Unreleased] → Added`.

---

## File Structure

New:
- `internal/api/sbom/project.go` — CycloneDX parsing + projection to the `Document` shape.
- `internal/api/sbom/project_test.go` — projection tests.
- `internal/api/sbom/testdata/example.cdx.json` — fixture.
- `internal/api/sbom/embed.go` — `//go:embed` + init projection; `Raw()`, `ProjectedJSON()`.
- `internal/api/sbom/sbom.cdx.json` — committed generated artifact.
- `internal/api/licenses.go` — two HTTP handlers.
- `internal/api/licenses_test.go` — handler tests.
- `scripts/build-sbom.sh` — generator.
- `.githooks/pre-commit` — opt-in regeneration hook.
- `frontend/src/pages/Licenses.tsx` — the page.
- `frontend/src/pages/Licenses.test.tsx` — page test.

Modified:
- `internal/api/router.go` — register `GET /-/licenses` and `GET /-/sbom.cdx.json`.
- `api/openapi.yaml` — document the two endpoints.
- `frontend/src/api/types.ts` — `LicensesResponse`, `LicenseComponent`, `LicenseEntry`.
- `frontend/src/api/client.ts` — `api.licenses()`.
- `frontend/src/App.tsx` — `/licenses` route + lazy import.
- `frontend/src/components/Footer.tsx` — footer link.
- `Makefile` — `sbom`, `sbom-check`, `hooks` targets.
- `.github/workflows/ci.yml` — `sbom-check` job.
- `CHANGELOG.md`.

---

## Task 1: SBOM projection logic (pure)

**Files:**
- Create: `internal/api/sbom/project.go`
- Create: `internal/api/sbom/testdata/example.cdx.json`
- Test: `internal/api/sbom/project_test.go`

**Interfaces:**
- Produces: `Project(raw []byte) (Document, error)`; types `Document{GeneratedAt string; Components []Component}`, `Component{Name, Version, Description, Ecosystem string; Licenses []License; Homepage, Repository string}`, `License{ID, Name, URL string}`.

- [ ] **Step 1: Write the fixture**

Create `internal/api/sbom/testdata/example.cdx.json` (mirrors a `cyclonedx merge --hierarchical` result: sub-BOM container components with nested `components`, plus one container lacking a purl to prove it is skipped):

```json
{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "version": 1,
  "components": [
    {
      "type": "application",
      "name": "go-modules",
      "components": [
        {
          "type": "library",
          "name": "github.com/mark3labs/mcp-go",
          "version": "v0.55.1",
          "description": "A Go implementation of the Model Context Protocol",
          "purl": "pkg:golang/github.com/mark3labs/mcp-go@v0.55.1",
          "licenses": [{ "license": { "id": "MIT", "url": "https://spdx.org/licenses/MIT.html" } }],
          "externalReferences": [
            { "type": "website", "url": "https://github.com/mark3labs/mcp-go" },
            { "type": "vcs", "url": "https://github.com/mark3labs/mcp-go" }
          ]
        }
      ]
    },
    {
      "type": "application",
      "name": "npm-packages",
      "components": [
        {
          "type": "library",
          "name": "react",
          "version": "19.0.0",
          "purl": "pkg:npm/react@19.0.0",
          "licenses": [{ "expression": "MIT" }]
        }
      ]
    },
    { "type": "application", "name": "container-without-purl" }
  ]
}
```

- [ ] **Step 2: Write the failing test**

Create `internal/api/sbom/project_test.go`:

```go
package sbom

import (
	"os"
	"testing"
)

func TestProjectFlattensAndMaps(t *testing.T) {
	raw, err := os.ReadFile("testdata/example.cdx.json")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := Project(raw)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	if len(doc.Components) != 2 {
		t.Fatalf("got %d components, want 2 (container without purl must be skipped)", len(doc.Components))
	}

	// Sorted: ecosystem "go" before "npm".
	goComp := doc.Components[0]
	if goComp.Ecosystem != "go" || goComp.Name != "github.com/mark3labs/mcp-go" {
		t.Fatalf("first component = %+v", goComp)
	}
	if len(goComp.Licenses) != 1 || goComp.Licenses[0].ID != "MIT" {
		t.Errorf("go licenses = %+v", goComp.Licenses)
	}
	if goComp.Homepage == "" || goComp.Repository == "" {
		t.Errorf("go refs: homepage=%q repository=%q", goComp.Homepage, goComp.Repository)
	}

	npmComp := doc.Components[1]
	if npmComp.Ecosystem != "npm" || npmComp.Name != "react" {
		t.Fatalf("second component = %+v", npmComp)
	}
	if len(npmComp.Licenses) != 1 || npmComp.Licenses[0].ID != "MIT" {
		t.Errorf("npm licenses (from expression) = %+v", npmComp.Licenses)
	}
}

func TestProjectInvalidJSON(t *testing.T) {
	if _, err := Project([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/api/sbom/`
Expected: FAIL — `undefined: Project` (and undefined types).

- [ ] **Step 4: Write the implementation**

Create `internal/api/sbom/project.go`:

```go
// Package sbom embeds the CycloneDX software bill of materials generated at
// build time (scripts/build-sbom.sh) and projects it into the flat licenses
// document served by the open-source licenses page.
package sbom

import (
	"encoding/json"
	"sort"
	"strings"
)

// Document is the projected licenses view served at GET /-/licenses.
type Document struct {
	GeneratedAt string      `json:"generatedAt,omitempty"`
	Components  []Component `json:"components"`
}

// Component is one open-source dependency bundled into Cetacean.
type Component struct {
	Name        string    `json:"name"`
	Version     string    `json:"version,omitempty"`
	Description string    `json:"description,omitempty"`
	Ecosystem   string    `json:"ecosystem"` // "go" | "npm" | "other"
	Licenses    []License `json:"licenses"`
	Homepage    string    `json:"homepage,omitempty"`
	Repository  string    `json:"repository,omitempty"`
}

// License is a single license entry for a component.
type License struct {
	ID   string `json:"id,omitempty"` // SPDX identifier or expression, when known
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// Minimal CycloneDX subset needed for projection.

type cycloneDX struct {
	Components []cdxComponent `json:"components"`
}

type cdxComponent struct {
	Name               string         `json:"name"`
	Version            string         `json:"version"`
	Description        string         `json:"description"`
	Purl               string         `json:"purl"`
	Licenses           []cdxLicense   `json:"licenses"`
	ExternalReferences []cdxExtRef    `json:"externalReferences"`
	Components         []cdxComponent `json:"components"`
}

type cdxLicense struct {
	License    *cdxLicenseChoice `json:"license"`
	Expression string            `json:"expression"`
}

type cdxLicenseChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type cdxExtRef struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// Project parses a CycloneDX JSON document and flattens it into a Document.
// Components lacking a recognized package URL (e.g. the synthetic container
// components a hierarchical merge produces) are skipped; nested components are
// walked recursively. Output is sorted by ecosystem, then name, so the
// projection is deterministic.
func Project(raw []byte) (Document, error) {
	var cdx cycloneDX
	if err := json.Unmarshal(raw, &cdx); err != nil {
		return Document{}, err
	}

	seen := make(map[string]struct{})
	components := make([]Component, 0, len(cdx.Components))

	var walk func(in []cdxComponent)
	walk = func(in []cdxComponent) {
		for _, component := range in {
			ecosystem := ecosystemFromPurl(component.Purl)
			if ecosystem != "" {
				key := component.Name + "@" + component.Version
				if _, duplicate := seen[key]; !duplicate {
					seen[key] = struct{}{}
					components = append(components, Component{
						Name:        component.Name,
						Version:     component.Version,
						Description: component.Description,
						Ecosystem:   ecosystem,
						Licenses:    projectLicenses(component.Licenses),
						Homepage:    externalReference(component.ExternalReferences, "website"),
						Repository:  externalReference(component.ExternalReferences, "vcs"),
					})
				}
			}

			if len(component.Components) > 0 {
				walk(component.Components)
			}
		}
	}
	walk(cdx.Components)

	sort.Slice(components, func(i, j int) bool {
		if components[i].Ecosystem != components[j].Ecosystem {
			return components[i].Ecosystem < components[j].Ecosystem
		}

		return strings.ToLower(components[i].Name) < strings.ToLower(components[j].Name)
	})

	return Document{Components: components}, nil
}

func ecosystemFromPurl(purl string) string {
	switch {
	case strings.HasPrefix(purl, "pkg:golang/"):
		return "go"
	case strings.HasPrefix(purl, "pkg:npm/"):
		return "npm"
	default:
		return ""
	}
}

func projectLicenses(in []cdxLicense) []License {
	out := make([]License, 0, len(in))
	for _, license := range in {
		switch {
		case license.License != nil:
			out = append(out, License{
				ID:   license.License.ID,
				Name: license.License.Name,
				URL:  license.License.URL,
			})
		case license.Expression != "":
			out = append(out, License{ID: license.Expression})
		}
	}

	return out
}

func externalReference(refs []cdxExtRef, kind string) string {
	for _, ref := range refs {
		if ref.Type == kind {
			return ref.URL
		}
	}

	return ""
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/sbom/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/sbom/project.go internal/api/sbom/project_test.go internal/api/sbom/testdata/example.cdx.json
git commit -m "feat(sbom): CycloneDX projection to flat licenses document"
```

---

## Task 2: Toolchain dependencies, generator script, Makefile targets, and the committed SBOM

**Files:**
- Modify: `go.mod`, `go.sum` (add the `cyclonedx-gomod` tool directive)
- Modify: `frontend/package.json`, `frontend/package-lock.json` (add `@cyclonedx/cyclonedx-npm` devDependency)
- Create: `scripts/build-sbom.sh`
- Create: `internal/api/sbom/sbom.cdx.json` (generated, committed)
- Modify: `Makefile`

**Interfaces:**
- Produces: `internal/api/sbom/sbom.cdx.json` (the file Task 3 embeds); `make sbom`, `make sbom-check`, `make hooks`.

- [ ] **Step 1: Register the Go generator as a tool dependency**

Run (from the repo root; verify the version against the latest release and pin it):

```bash
go get -tool github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.9.0
```

Expected: `go.mod` gains a `tool github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod` directive (plus `require` entries), and `go.sum` is updated. Verify the tool resolves:

```bash
go tool cyclonedx-gomod version
```

Expected: prints a version, compiling the tool from the module cache on first run.

- [ ] **Step 2: Register the npm generator as a frontend devDependency**

Run (verify/pin the version):

```bash
cd frontend && npm install --save-dev @cyclonedx/cyclonedx-npm@^5 && cd ..
```

Expected: `frontend/package.json` `devDependencies` gains `@cyclonedx/cyclonedx-npm`; `frontend/package-lock.json` is updated. Verify it resolves locally:

```bash
cd frontend && npx --no-install cyclonedx-npm --version && cd ..
```

Expected: prints a version (no network fetch, because `--no-install` uses the declared dependency).

- [ ] **Step 3: Write the generator script**

Create `scripts/build-sbom.sh`. It invokes the tools through their declared-dependency entry points (`go tool …`, `npx --no-install …`) and merges + strips volatile fields with `jq` (no `cyclonedx-cli`):

```bash
#!/usr/bin/env bash
# Generates the CycloneDX SBOM embedded into the binary and consumed by the
# open-source licenses page. Covers what the cetacean binary imports (Go, via
# `cyclonedx-gomod app`) plus frontend npm production dependencies. The two
# CycloneDX documents are merged with jq into a fresh envelope with no volatile
# fields (serialNumber, timestamp) so the committed file only changes when
# dependencies change.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$repo_root/internal/api/sbom/sbom.cdx.json"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

version="$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || echo dev)"

command -v jq >/dev/null 2>&1 || {
  echo "error: 'jq' not found (system prerequisite). Install: https://jqlang.github.io/jq/" >&2
  exit 1
}

echo "==> Go modules (binary imports only)"
( cd "$repo_root" && go tool cyclonedx-gomod app -main . -json -licenses -output "$tmp/go.cdx.json" . )

echo "==> npm packages (production only)"
( cd "$repo_root/frontend" && npx --no-install cyclonedx-npm --omit dev --output-file "$tmp/npm.cdx.json" )

echo "==> merge + strip volatile fields (deterministic output)"
jq -s --arg version "$version" '
  {
    bomFormat: "CycloneDX",
    specVersion: (.[0].specVersion // "1.6"),
    version: 1,
    metadata: { component: { type: "application", name: "cetacean", version: $version } },
    components: ((.[0].components // []) + (.[1].components // []))
  }
' "$tmp/go.cdx.json" "$tmp/npm.cdx.json" > "$out"

echo "wrote $out"
```

- [ ] **Step 4: Make it executable**

Run: `chmod +x scripts/build-sbom.sh`

- [ ] **Step 5: Add Makefile targets**

In `Makefile`, add `sbom sbom-check hooks` to the `.PHONY` line, and append these targets:

```makefile
## Generate the CycloneDX SBOM (Go + frontend npm) embedded into the binary
sbom:
	./scripts/build-sbom.sh

## Verify the committed SBOM is up to date (CI gate)
sbom-check: sbom
	@git diff --exit-code -- internal/api/sbom/sbom.cdx.json \
	  || { echo "ERROR: internal/api/sbom/sbom.cdx.json is stale. Run 'make sbom' and commit." >&2; exit 1; }

## Install the repository git hooks (opt-in)
hooks:
	git config core.hooksPath .githooks
	@echo "git hooks installed (core.hooksPath=.githooks)"
```

Note: `make check` is intentionally left unchanged (it must not require the SBOM toolchain for every local run); `sbom-check` runs as a dedicated CI job (Task 8).

- [ ] **Step 6: Generate the SBOM**

Ensure frontend deps are installed (`cd frontend && npm ci && cd ..`), then run: `make sbom`
Expected: `wrote .../internal/api/sbom/sbom.cdx.json`. Verify:
```bash
jq '.components | length' internal/api/sbom/sbom.cdx.json
grep -c 'pkg:npm/' internal/api/sbom/sbom.cdx.json
grep -c 'pkg:golang/' internal/api/sbom/sbom.cdx.json
```
Expected: non-zero counts for all three. **Accuracy check:** confirm the SBOM does *not* list `cyclonedx-gomod`'s own toolchain dependencies (e.g. `grep -c 'cyclonedx-gomod' internal/api/sbom/sbom.cdx.json` should be 0). If tool deps leak in, the `app -main .` invocation is not scoping correctly — stop and report before committing.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum frontend/package.json frontend/package-lock.json \
        scripts/build-sbom.sh Makefile internal/api/sbom/sbom.cdx.json
git commit -m "build(sbom): declare cyclonedx tools as deps, generator, committed SBOM"
```

---

## Task 3: Embed the SBOM and expose Raw/ProjectedJSON

**Files:**
- Create: `internal/api/sbom/embed.go`
- Test: `internal/api/sbom/embed_test.go`

**Interfaces:**
- Consumes: `Project` (Task 1), the committed `sbom.cdx.json` (Task 2), `internal/version`.
- Produces: `Raw() []byte`, `ProjectedJSON() []byte`.

- [ ] **Step 1: Write the failing test**

Create `internal/api/sbom/embed_test.go`:

```go
package sbom

import (
	"encoding/json"
	"testing"
)

func TestRawIsValidCycloneDX(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(Raw(), &doc); err != nil {
		t.Fatalf("embedded SBOM is not valid JSON: %v", err)
	}
	if doc["bomFormat"] != "CycloneDX" {
		t.Errorf("bomFormat = %v, want CycloneDX", doc["bomFormat"])
	}
}

func TestProjectedJSONHasComponents(t *testing.T) {
	var doc Document
	if err := json.Unmarshal(ProjectedJSON(), &doc); err != nil {
		t.Fatalf("projected JSON invalid: %v", err)
	}
	if len(doc.Components) == 0 {
		t.Error("expected at least one projected component from the committed SBOM")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/sbom/ -run 'Raw|ProjectedJSON'`
Expected: FAIL — `undefined: Raw` / `undefined: ProjectedJSON`.

- [ ] **Step 3: Write the implementation**

Create `internal/api/sbom/embed.go`:

```go
package sbom

import (
	_ "embed"
	"encoding/json"
	"log/slog"

	"github.com/radiergummi/cetacean/internal/version"
)

//go:embed sbom.cdx.json
var raw []byte

var projectedJSON []byte

func init() {
	doc, err := Project(raw)
	if err != nil {
		slog.Warn("sbom: could not project embedded CycloneDX; licenses page will be empty", "error", err)
		doc = Document{Components: []Component{}}
	}

	if version.Date != "" && version.Date != "unknown" {
		doc.GeneratedAt = version.Date
	}

	body, err := json.Marshal(doc)
	if err != nil {
		slog.Warn("sbom: could not marshal projected document", "error", err)
		body = []byte(`{"components":[]}`)
	}
	projectedJSON = body
}

// Raw returns the embedded CycloneDX SBOM bytes.
func Raw() []byte { return raw }

// ProjectedJSON returns the flattened licenses Document as JSON, computed once
// at package initialization.
func ProjectedJSON() []byte { return projectedJSON }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/sbom/`
Expected: PASS (all tests, including Task 1's).

- [ ] **Step 5: Commit**

```bash
git add internal/api/sbom/embed.go internal/api/sbom/embed_test.go
git commit -m "feat(sbom): embed SBOM and expose Raw/ProjectedJSON"
```

---

## Task 4: HTTP handlers, routes, and OpenAPI docs

**Files:**
- Create: `internal/api/licenses.go`
- Test: `internal/api/licenses_test.go`
- Modify: `internal/api/router.go` (near the meta endpoints, ~line 133)
- Modify: `api/openapi.yaml`

**Interfaces:**
- Consumes: `sbom.Raw()`, `sbom.ProjectedJSON()` (Task 3); `writeRawWithETag` (existing, `internal/api/etag.go`).
- Produces: `HandleLicenses`, `HandleSBOM` (both `http.HandlerFunc`); routes `GET /-/licenses`, `GET /-/sbom.cdx.json`.

- [ ] **Step 1: Write the failing test**

Create `internal/api/licenses_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLicensesServesProjectedJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/-/licenses", nil)
	rec := httptest.NewRecorder()

	HandleLicenses(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var body struct {
		Components []map[string]any `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if len(body.Components) == 0 {
		t.Error("expected components in projected licenses response")
	}
}

func TestHandleSBOMServesCycloneDXWithETag304(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/-/sbom.cdx.json", nil)
	rec := httptest.NewRecorder()

	HandleSBOM(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header")
	}

	// Conditional request with the same ETag must yield 304.
	req2 := httptest.NewRequest(http.MethodGet, "/-/sbom.cdx.json", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()

	HandleSBOM(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", rec2.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run 'HandleLicenses|HandleSBOM'`
Expected: FAIL — `undefined: HandleLicenses` / `undefined: HandleSBOM`.

- [ ] **Step 3: Write the handlers**

Create `internal/api/licenses.go`:

```go
package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/api/sbom"
)

// HandleLicenses serves the projected open-source licenses document consumed by
// the licenses page. Public (registered under the auth-exempt /-/ prefix).
func HandleLicenses(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	writeRawWithETag(w, r, sbom.ProjectedJSON())
}

// HandleSBOM serves the raw embedded CycloneDX SBOM for supply-chain tooling.
// Public (registered under the auth-exempt /-/ prefix).
func HandleSBOM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.cyclonedx+json; version=1.6")
	writeRawWithETag(w, r, sbom.Raw())
}
```

- [ ] **Step 4: Register the routes**

In `internal/api/router.go`, in the meta-endpoints block (immediately after the `GET /-/docker-latest-version` line, ~line 135), add:

```go
	mux.HandleFunc("GET /-/licenses", HandleLicenses)
	mux.HandleFunc("GET /-/sbom.cdx.json", HandleSBOM)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'HandleLicenses|HandleSBOM'`
Expected: PASS.

- [ ] **Step 6: Document the endpoints in OpenAPI**

In `api/openapi.yaml`, under `paths:`, add (indentation matching the file's other path entries):

```yaml
  /-/licenses:
    get:
      summary: Open-source licenses (projected)
      description: Flattened list of bundled open-source components (Go modules and frontend npm packages) with license metadata. Public.
      responses:
        "200":
          description: Projected licenses document
          content:
            application/json:
              schema:
                type: object
                required: [components]
                properties:
                  generatedAt: { type: string }
                  components:
                    type: array
                    items:
                      type: object
                      required: [name, ecosystem, licenses]
                      properties:
                        name: { type: string }
                        version: { type: string }
                        description: { type: string }
                        ecosystem: { type: string, enum: [go, npm, other] }
                        homepage: { type: string, format: uri }
                        repository: { type: string, format: uri }
                        licenses:
                          type: array
                          items:
                            type: object
                            properties:
                              id: { type: string }
                              name: { type: string }
                              url: { type: string, format: uri }
  /-/sbom.cdx.json:
    get:
      summary: Software Bill of Materials (CycloneDX)
      description: Raw CycloneDX SBOM for Go and frontend npm dependencies. Public.
      responses:
        "200":
          description: CycloneDX JSON document
          content:
            application/vnd.cyclonedx+json:
              schema:
                type: object
```

- [ ] **Step 7: Verify the spec still parses and build is green**

Run: `go build ./... && go test ./internal/api/ -run 'HandleLicenses|HandleSBOM'`
Expected: build succeeds (HandleAPIDoc panics at startup if the YAML is invalid, so a clean `go build` plus a quick `go run . --version` is enough confidence); tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/api/licenses.go internal/api/licenses_test.go internal/api/router.go api/openapi.yaml
git commit -m "feat(api): public /-/licenses and /-/sbom.cdx.json endpoints"
```

---

## Task 5: Frontend types and API client method

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

**Interfaces:**
- Produces: TS types `LicenseEntry`, `LicenseComponent`, `LicensesResponse`; `api.licenses(signal?)` returning `Promise<LicensesResponse>`.

- [ ] **Step 1: Add the types**

Append to `frontend/src/api/types.ts`:

```ts
export interface LicenseEntry {
  id?: string;
  name?: string;
  url?: string;
}

export interface LicenseComponent {
  name: string;
  version?: string;
  description?: string;
  ecosystem: string;
  licenses: LicenseEntry[];
  homepage?: string;
  repository?: string;
}

export interface LicensesResponse {
  generatedAt?: string;
  components: LicenseComponent[];
}
```

- [ ] **Step 2: Add the client method**

In `frontend/src/api/client.ts`, add `LicensesResponse` to the existing type import from `./types`, and add this method inside the `export const api = { … }` object (next to `health`, ~line 775):

```ts
  licenses: (signal?: AbortSignal) =>
    fetchJSON<LicensesResponse>(`/-/licenses`, signal).then(({ data }) => data),
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat(frontend): licenses API types and client method"
```

---

## Task 6: Licenses page, route, and footer link

**Files:**
- Create: `frontend/src/pages/Licenses.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/components/Footer.tsx`

**Interfaces:**
- Consumes: `api.licenses` and the types (Task 5); existing `PageHeader`, `FetchError`, `ui/input`, `ui/badge`, `ui/card`, `getErrorMessage`.
- Produces: default-exported `Licenses` page; `/licenses` route; footer link.

- [ ] **Step 1: Write the page**

Create `frontend/src/pages/Licenses.tsx`:

```tsx
import { api } from "@/api/client";
import type { LicenseComponent, LicensesResponse } from "@/api/types";
import FetchError from "@/components/FetchError";
import PageHeader from "@/components/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { getErrorMessage } from "@/lib/utils";
import { ExternalLink } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

type EcosystemFilter = "all" | "go" | "npm" | "other";

const ecosystemLabels: Record<string, string> = {
  go: "Go",
  npm: "npm",
  other: "Other",
};

export default function Licenses() {
  const [data, setData] = useState<LicensesResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [ecosystem, setEcosystem] = useState<EcosystemFilter>("all");

  useEffect(() => {
    const controller = new AbortController();

    api
      .licenses(controller.signal)
      .then((response) => {
        setData(response);
        setError(null);
      })
      .catch((caught) => {
        if (!controller.signal.aborted) {
          setError(getErrorMessage(caught));
        }
      });

    return () => controller.abort();
  }, []);

  const components = data?.components ?? [];

  const counts = useMemo(() => {
    const result: Record<string, number> = { all: components.length };

    for (const component of components) {
      result[component.ecosystem] = (result[component.ecosystem] ?? 0) + 1;
    }

    return result;
  }, [components]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();

    return components.filter((component) => {
      if (ecosystem !== "all" && component.ecosystem !== ecosystem) {
        return false;
      }

      if (needle && !component.name.toLowerCase().includes(needle)) {
        return false;
      }

      return true;
    });
  }, [components, query, ecosystem]);

  if (error) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <FetchError message={error} />
      </div>
    );
  }

  const ecosystems: EcosystemFilter[] = ["all", "go", "npm"];

  if (counts.other) {
    ecosystems.push("other");
  }

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      <PageHeader
        title="Open-source licenses"
        subtitle="Every open-source project bundled into Cetacean."
      />

      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <Input
          type="search"
          placeholder="Search by name…"
          value={query}
          onChange={({ target }) => setQuery(target.value)}
          className="sm:max-w-xs"
        />

        <div className="flex flex-wrap gap-2">
          {ecosystems.map((value) => (
            <button
              key={value}
              type="button"
              onClick={() => setEcosystem(value)}
              className={
                value === ecosystem
                  ? "rounded-md bg-primary px-3 py-1 text-xs font-medium text-primary-foreground"
                  : "rounded-md border px-3 py-1 text-xs text-muted-foreground transition hover:text-foreground"
              }
            >
              {value === "all" ? "All" : ecosystemLabels[value]}
              <span className="ml-1.5 opacity-60">{counts[value] ?? 0}</span>
            </button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {filtered.map((component) => (
          <LicenseCard
            key={`${component.ecosystem}:${component.name}@${component.version ?? ""}`}
            component={component}
          />
        ))}
      </div>

      {data && filtered.length === 0 && (
        <p className="py-12 text-center text-sm text-muted-foreground">No matching components.</p>
      )}
    </div>
  );
}

function LicenseCard({ component }: { component: LicenseComponent }) {
  return (
    <Card className="flex flex-col gap-2 p-4">
      <div className="flex items-start justify-between gap-2">
        {component.homepage ? (
          <a
            href={component.homepage}
            target="_blank"
            rel="noopener noreferrer"
            className="truncate font-medium transition hover:text-primary"
          >
            {component.name}
          </a>
        ) : (
          <span className="truncate font-medium">{component.name}</span>
        )}

        <Badge variant="outline">
          {ecosystemLabels[component.ecosystem] ?? component.ecosystem}
        </Badge>
      </div>

      {component.version && (
        <span className="font-mono text-xs text-muted-foreground">{component.version}</span>
      )}

      {component.description && (
        <p className="line-clamp-2 text-xs text-muted-foreground">{component.description}</p>
      )}

      <div className="mt-auto flex flex-wrap items-center gap-1.5 pt-1">
        {component.licenses.map((license, index) => (
          <Badge
            key={index}
            variant="secondary"
          >
            {license.id || license.name || "Unknown"}
          </Badge>
        ))}

        {component.repository && (
          <a
            href={component.repository}
            target="_blank"
            rel="noopener noreferrer"
            className="ml-auto inline-flex items-center gap-1 text-xs text-muted-foreground transition hover:text-foreground"
          >
            <ExternalLink className="size-3" />
            Source
          </a>
        )}
      </div>
    </Card>
  );
}
```

- [ ] **Step 2: Register the route**

In `frontend/src/App.tsx`, add a lazy import near the other page imports:

```tsx
const Licenses = lazy(() => import("./pages/Licenses"));
```

and a route inside `<Routes>` (next to `/search`, ~line 391):

```tsx
                  <Route
                    path="/licenses"
                    element={<Licenses />}
                  />
```

- [ ] **Step 3: Add the footer link**

In `frontend/src/components/Footer.tsx`: add `Link` to the react-router import and `Scale` to the lucide import, then add this as the first item inside the footer `<nav>` (before the GitHub link):

```tsx
          <Link
            to="/licenses"
            className="inline-flex items-center gap-1 transition hover:text-foreground"
          >
            <Scale className="size-3.5" />
            Licenses
          </Link>
```

Add the imports at the top of the file:

```tsx
import { Book, ExternalLink, Scale } from "lucide-react";
import { Link } from "react-router-dom";
```

- [ ] **Step 4: Type-check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npx oxlint`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/Licenses.tsx frontend/src/App.tsx frontend/src/components/Footer.tsx
git commit -m "feat(frontend): open-source licenses page + footer link"
```

---

## Task 7: Frontend page test

**Files:**
- Create: `frontend/src/pages/Licenses.test.tsx`

**Interfaces:**
- Consumes: the `Licenses` page (Task 6); mocks `@/api/client`.

- [ ] **Step 1: Write the test**

Create `frontend/src/pages/Licenses.test.tsx`:

```tsx
import { api } from "@/api/client";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Licenses from "./Licenses";

vi.mock("@/api/client", () => ({
  api: { licenses: vi.fn() },
}));

const sample = {
  components: [
    { name: "github.com/foo/bar", version: "v1.0.0", ecosystem: "go", licenses: [{ id: "MIT" }] },
    { name: "react", version: "19.0.0", ecosystem: "npm", licenses: [{ id: "MIT" }] },
    { name: "react-dom", version: "19.0.0", ecosystem: "npm", licenses: [{ id: "MIT" }] },
  ],
};

describe("Licenses", () => {
  beforeEach(() => {
    (api.licenses as ReturnType<typeof vi.fn>).mockResolvedValue(sample);
  });

  it("renders all components then filters by ecosystem", async () => {
    render(<Licenses />);

    await waitFor(() => expect(screen.getByText("react")).toBeInTheDocument());
    expect(screen.getByText("github.com/foo/bar")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^npm/i }));

    expect(screen.queryByText("github.com/foo/bar")).not.toBeInTheDocument();
    expect(screen.getByText("react-dom")).toBeInTheDocument();
  });

  it("filters by search query", async () => {
    render(<Licenses />);

    await waitFor(() => expect(screen.getByText("react-dom")).toBeInTheDocument());

    fireEvent.change(screen.getByPlaceholderText(/search by name/i), {
      target: { value: "react-dom" },
    });

    expect(screen.queryByText("github.com/foo/bar")).not.toBeInTheDocument();
    expect(screen.getByText("react-dom")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test**

Run: `cd frontend && npx vitest run src/pages/Licenses.test.tsx`
Expected: PASS (2 tests).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/Licenses.test.tsx
git commit -m "test(frontend): licenses page search and ecosystem filter"
```

---

## Task 8: Pre-commit hook and CI gate

**Files:**
- Create: `.githooks/pre-commit`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `make sbom` / `make sbom-check` (Task 2).

- [ ] **Step 1: Write the hook**

Create `.githooks/pre-commit`:

```bash
#!/usr/bin/env bash
# Regenerate the committed SBOM when a dependency manifest is staged, so it
# never drifts from the lockfiles. No-ops for commits that don't touch
# dependencies, and skips gracefully when the SBOM toolchain is absent (CI is
# the real gate). Install with: make hooks
set -euo pipefail

staged="$(git diff --cached --name-only)"
case "$staged" in
  *go.mod*|*go.sum*|*frontend/package.json*|*frontend/package-lock.json*) ;;
  *) exit 0 ;;
esac

# The generators are declared deps (go tool cyclonedx-gomod; npx cyclonedx-npm),
# so we only need go, node/npx, jq, and installed frontend deps. Skip (don't
# block) when anything is missing — CI is the real gate.
for tool in go npx jq; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "pre-commit: '$tool' missing; skipping SBOM regeneration (CI will verify)." >&2
    exit 0
  fi
done

if [ ! -d frontend/node_modules ]; then
  echo "pre-commit: frontend/node_modules missing; run 'cd frontend && npm ci'. Skipping SBOM regeneration (CI will verify)." >&2
  exit 0
fi

echo "pre-commit: dependency manifest changed, regenerating SBOM…"
make sbom
git add internal/api/sbom/sbom.cdx.json
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x .githooks/pre-commit`

- [ ] **Step 3: Add the CI job**

In `.github/workflows/ci.yml`, add a new job (reuse the pinned action SHAs already present in the file):

```yaml
  sbom-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6.0.2
      - uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0
        with:
          go-version-file: go.mod
      - uses: actions/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e # v6.4.0
        with:
          node-version: 24
          cache: npm
          cache-dependency-path: frontend/package-lock.json
      - run: cd frontend && npm ci
      - run: make sbom-check
```

Both generators are declared dependencies — `cyclonedx-gomod` resolves via the `go.mod` tool directive (`go tool cyclonedx-gomod`), and `cyclonedx-npm` is installed by `npm ci` (frontend devDependency). `jq` is preinstalled on `ubuntu-latest`. No ad-hoc installs or binary downloads are needed.

- [ ] **Step 4: Verify the hook locally**

Run:
```bash
make hooks
# no-op path: a commit touching an unrelated file must not regenerate
git commit --allow-empty -m "chore: verify hook no-ops" && git reset --soft HEAD~1
```
Expected: the hook prints nothing (no "regenerating SBOM") for a commit that stages no dependency manifest.

- [ ] **Step 5: Commit**

```bash
git add .githooks/pre-commit .github/workflows/ci.yml
git commit -m "ci(sbom): pre-commit regeneration hook + sbom-check gate"
```

---

## Task 9: Changelog and end-to-end verification

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Add the changelog entry**

Under `## [Unreleased] → ### Added` in `CHANGELOG.md`, add:

```markdown
- Open-source licenses page (linked from the footer) listing every Go module and frontend dependency bundled into Cetacean, with search and per-ecosystem filtering. The underlying software bill of materials is available as CycloneDX at `/-/sbom.cdx.json`.
```

- [ ] **Step 2: Full build and test**

Run:
```bash
cd frontend && npm run build && cd ..
go build -o cetacean . && go test ./...
```
Expected: build succeeds; all Go tests pass.

- [ ] **Step 3: Manual smoke test**

Run (in one shell):
```bash
CETACEAN_DOCKER_HOST="unix:///nonexistent.sock" CETACEAN_LISTEN_ADDR=":19557" ./cetacean &
sleep 2
curl -s -o /dev/null -w "licenses=%{http_code} %{content_type}\n" http://localhost:19557/-/licenses
curl -s -o /dev/null -w "sbom=%{http_code} %{content_type}\n" http://localhost:19557/-/sbom.cdx.json
curl -s http://localhost:19557/-/licenses | jq '.components | length'
kill %1
```
Expected: both endpoints `200`; `/-/licenses` is `application/json`; `/-/sbom.cdx.json` is `application/vnd.cyclonedx+json`; component count is non-zero.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: changelog for open-source licenses page"
```

---

## Notes / intentional deviations from the spec

- **Toolchain declared as dependencies (post-spec refinement).** Per the go-ahead to install the toolchain as normal dependencies: `cyclonedx-gomod` is a `go.mod` tool directive and `cyclonedx-npm` a frontend devDependency, replacing the spec's ad-hoc `go install` / `npm -g`. The spec's `cyclonedx-cli` merge step is **dropped** — the merge is done with `jq` (already required for the strip), removing the one tool that fit neither Go nor npm. Consequently the Go generator uses `cyclonedx-gomod app -main .` (binary imports only) instead of `mod`, so the tool directive's own dependency tree does not appear on the licenses page.
- **`make check` is not extended with `sbom-check`.** The spec mentioned folding it in; doing so would force the SBOM toolchain onto every local `make check`. Instead the gate runs as a dedicated CI job (`sbom-check`), which is the part that actually enforces freshness. Local devs get the opt-in pre-commit hook.
- **Handlers reuse `writeRawWithETag`** (existing helper) rather than introducing custom `Cache-Control: public, max-age=300` headers. The helper sets `Cache-Control: no-cache` + `ETag`, giving conditional-revalidation caching, which is the right fit for a versioned artifact and keeps the code consistent with the rest of `internal/api`.
- **`generatedAt`** is populated from `internal/version.Date` at init (omitted when `unknown`), since the SBOM's own timestamp is stripped for deterministic diffs. `Project()` itself stays pure (leaves `GeneratedAt` empty) so it is trivially testable.
