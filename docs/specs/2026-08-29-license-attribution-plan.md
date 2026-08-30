# License Attribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harvest every bundled dependency's license text and `NOTICE` file into the binary, serve them from the licenses page, and generate `THIRD_PARTY_LICENSES` from them.

**Architecture:** A Go generator walks the committed CycloneDX SBOM, resolves each component to its source directory in the Go module cache or `frontend/node_modules`, and writes a second committed artifact holding a content-addressed pool of license texts plus a component→text mapping. That artifact is embedded next to the SBOM. The licenses projection gains `textId`/`noticeId` per component; two new public endpoints serve individual texts and the full attribution document. The page gains a license filter and a lazily-fetched text dialog.

**Tech Stack:** Go (`crypto/sha256`, `encoding/json`, `path/filepath`, `//go:embed`), stdlib `net/http.ServeMux`, React 19 + TypeScript, TanStack Query, Vitest, and the repo's `Combobox` and `Dialog` wrappers over `@base-ui/react`.

**Spec:** `docs/specs/2026-08-29-license-attribution-design.md`

## Global Constraints

- **Deterministic output.** Both committed artifacts must be byte-identical across runs and machines. `make sbom-check` diffs them in CI; any nondeterminism (map iteration order, timestamps, absolute paths, CRLF) breaks the build for everyone.
- **Generation fails loudly.** A component with no discoverable license file aborts generation naming the component. All 137 current components resolve; silence is how attribution rots.
- **Public endpoints.** Everything new lives under the auth-exempt `/-/` prefix, consistent with the 2026-07-01 decision that attribution data is public.
- **No abbreviations in TypeScript identifiers.** `formatLicense` not `fmtLic`. Industry-standard acronyms (`URL`, `API`, `SPDX`) are fine.
- **TSX formatting:** brace single-statement `if` bodies; blank lines after `if` blocks and variable declarations; JSX props on separate lines at 3+ props; camelCase module-level constants with `as const`.
- **CycloneDX stays untouched.** `sbom.cdx.json` must remain a valid, unmodified CycloneDX document for external tooling. All new data goes in the separate artifact.
- **Changelog.** User-facing changes get an entry under `[Unreleased]` in `CHANGELOG.md`, written from a dashboard user's perspective.

## File Structure

| File | Responsibility |
|---|---|
| `scripts/licensetexts/main.go` (create) | CLI entry: read SBOM, harvest, write artifact. Flags for input/output/roots so tests drive it. |
| `scripts/licensetexts/harvest.go` (create) | Pure harvesting logic: path resolution, file discovery, dedup pool. No I/O policy, no flags. |
| `scripts/licensetexts/harvest_test.go` (create) | Table tests over a fixture tree. |
| `scripts/licensetexts/testdata/` (create) | Fixture module cache + node_modules trees. |
| `internal/api/sbom/licensetexts.json` (create, generated) | Committed artifact: text pool + component mapping. |
| `internal/api/sbom/texts.go` (create) | Embeds the artifact; `Text(id)`, `Notice(id)`, `Lookup(key)`. |
| `internal/api/sbom/project.go` (modify) | `Component` gains `TextID`/`NoticeID`; `Project` populates them. |
| `internal/api/licenses.go` (modify) | Adds `HandleLicenseText` and `HandleNotices`. |
| `internal/api/router.go:136-139` (modify) | Registers the two new routes. |
| `THIRD_PARTY_LICENSES.preamble` (create) | Hand-curated header: Lucide/Feather derivation + the three curated license notes. |
| `THIRD_PARTY_LICENSES` (modify, generated) | preamble + generated per-component body. |
| `frontend/src/pages/Licenses.tsx` (modify) | License filter + text dialog + notices link. |
| `frontend/src/components/LicenseTextDialog.tsx` (create) | Fetches and renders one component's license and notice. |

Splitting `harvest.go` from `main.go` is what makes the generator testable: the harvesting logic takes root directories as parameters, so tests point it at fixtures instead of the real module cache.

---

### Task 1: License-text harvester

**Files:**
- Create: `internal/api/sbom/artifact.go`
- Create: `scripts/licensetexts/harvest.go`
- Create: `scripts/licensetexts/harvest_test.go`
- Create: `scripts/licensetexts/testdata/gomodcache/example.com/!upper!case@v1.0.0/LICENSE`
- Create: `scripts/licensetexts/testdata/gomodcache/example.com/plain@v1.0.0/LICENSE`
- Create: `scripts/licensetexts/testdata/gomodcache/example.com/noticed@v1.0.0/LICENSE`
- Create: `scripts/licensetexts/testdata/gomodcache/example.com/noticed@v1.0.0/NOTICE`
- Create: `scripts/licensetexts/testdata/node_modules/@scope/pkg/LICENSE`

**Interfaces:**
- Consumes: `sbom.Document` and `sbom.Component` from `internal/api/sbom` (fields `Name`, `Version`, `Ecosystem`).
- Produces, in `internal/api/sbom`: `type Artifact struct { Texts map[string]string; Components map[string]ComponentTexts }`, `type ComponentTexts struct { License, Notice string }`, `func ComponentKey(c Component) string`.
- Produces, in `scripts/licensetexts`: `func Harvest(doc sbom.Document, roots Roots) (sbom.Artifact, error)`, `type Roots struct { GoModCache, NodeModules string }`, `func escapeModulePath(module string) string`.

The artifact shape lives in `internal/api/sbom` and the generator imports it, so the writer and the reader cannot drift apart. `scripts/licensetexts` is `package main` and cannot be imported the other way round, which is why the direction matters.

- [ ] **Step 1: Define the shared artifact shape**

Create `internal/api/sbom/artifact.go`:

```go
package sbom

// ComponentTexts holds the pool ids of one component's license and notice.
type ComponentTexts struct {
	License string `json:"license"`
	Notice  string `json:"notice,omitempty"`
}

// Artifact is the licensetexts.json document that sits beside the SBOM: a
// content-addressed pool of license and notice texts, plus the mapping from
// component identity into it. Written by scripts/licensetexts, read by this
// package — the shape is declared here so the two cannot drift.
type Artifact struct {
	Texts      map[string]string         `json:"texts"`
	Components map[string]ComponentTexts `json:"components"`
}

// ComponentKey is a component's identity in the artifact. It matches the key
// the projection deduplicates on, so the two artifacts join cleanly.
func ComponentKey(component Component) string {
	return component.Ecosystem + ":" + component.Name + "@" + component.Version
}
```

Run: `go build ./internal/api/sbom/`
Expected: no output.

- [ ] **Step 2: Create the fixture tree**

```bash
mkdir -p scripts/licensetexts/testdata/gomodcache/example.com/'!upper!case@v1.0.0'
mkdir -p scripts/licensetexts/testdata/gomodcache/example.com/plain@v1.0.0
mkdir -p scripts/licensetexts/testdata/gomodcache/example.com/noticed@v1.0.0
mkdir -p scripts/licensetexts/testdata/node_modules/@scope/pkg

printf 'MIT License\n\nCopyright (c) 2026 Upper Case\n' \
  > scripts/licensetexts/testdata/gomodcache/example.com/'!upper!case@v1.0.0'/LICENSE
printf 'Shared boilerplate license text.\n' \
  > scripts/licensetexts/testdata/gomodcache/example.com/plain@v1.0.0/LICENSE
printf 'Shared boilerplate license text.\n' \
  > scripts/licensetexts/testdata/gomodcache/example.com/noticed@v1.0.0/LICENSE
printf 'Noticed Project\nCopyright 2026 The Noticed Authors\n' \
  > scripts/licensetexts/testdata/gomodcache/example.com/noticed@v1.0.0/NOTICE
printf 'ISC License\r\n\r\nCopyright (c) 2026 Scope\r\n' \
  > scripts/licensetexts/testdata/node_modules/@scope/pkg/LICENSE
```

The `plain` and `noticed` modules deliberately share byte-identical license text, so the test can prove deduplication. The npm fixture uses CRLF, so the test can prove line-ending normalization.

- [ ] **Step 3: Write the failing test**

Create `scripts/licensetexts/harvest_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/api/sbom"
)

func testRoots() Roots {
	return Roots{
		GoModCache:  "testdata/gomodcache",
		NodeModules: "testdata/node_modules",
	}
}

func TestHarvestResolvesEscapedModulePaths(t *testing.T) {
	// The Go module cache lowercases uppercase letters and prefixes them with
	// "!", so github.com/BurntSushi/toml lives at github.com/!burnt!sushi/toml.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "example.com/UpperCase", Version: "v1.0.0", Ecosystem: "go"},
	}}

	artifact, err := Harvest(doc, testRoots())
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}

	entry, ok := artifact.Components["go:example.com/UpperCase@v1.0.0"]
	if !ok {
		t.Fatalf("component missing from artifact: %+v", artifact.Components)
	}

	if got := artifact.Texts[entry.License]; got != "MIT License\n\nCopyright (c) 2026 Upper Case\n" {
		t.Errorf("license text = %q", got)
	}
}

func TestHarvestDeduplicatesIdenticalTexts(t *testing.T) {
	// Apache-2.0's text is byte-identical across every module that uses it.
	// Pooling by content hash is what keeps the artifact from tripling in size.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "example.com/plain", Version: "v1.0.0", Ecosystem: "go"},
		{Name: "example.com/noticed", Version: "v1.0.0", Ecosystem: "go"},
	}}

	artifact, err := Harvest(doc, testRoots())
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}

	plain := artifact.Components["go:example.com/plain@v1.0.0"]
	noticed := artifact.Components["go:example.com/noticed@v1.0.0"]

	if plain.License != noticed.License {
		t.Errorf("identical texts got distinct ids: %q vs %q", plain.License, noticed.License)
	}

	if len(artifact.Texts) != 2 {
		t.Errorf("pool holds %d texts, want 2 (one license + one notice)", len(artifact.Texts))
	}
}

func TestHarvestCapturesNoticeFiles(t *testing.T) {
	// Apache-2.0 section 4(d) makes propagating NOTICE mandatory.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "example.com/noticed", Version: "v1.0.0", Ecosystem: "go"},
		{Name: "example.com/plain", Version: "v1.0.0", Ecosystem: "go"},
	}}

	artifact, err := Harvest(doc, testRoots())
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}

	noticed := artifact.Components["go:example.com/noticed@v1.0.0"]
	if noticed.Notice == "" {
		t.Fatal("noticed module has a NOTICE file but no notice id")
	}

	if got := artifact.Texts[noticed.Notice]; got != "Noticed Project\nCopyright 2026 The Noticed Authors\n" {
		t.Errorf("notice text = %q", got)
	}

	if plain := artifact.Components["go:example.com/plain@v1.0.0"]; plain.Notice != "" {
		t.Errorf("module without a NOTICE got notice id %q", plain.Notice)
	}
}

func TestHarvestNormalizesLineEndings(t *testing.T) {
	// A CRLF license file would otherwise produce a different hash — and a
	// different committed artifact — depending on who checked the repo out.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "@scope/pkg", Version: "1.0.0", Ecosystem: "npm"},
	}}

	artifact, err := Harvest(doc, testRoots())
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}

	entry := artifact.Components["npm:@scope/pkg@1.0.0"]
	if got := artifact.Texts[entry.License]; got != "ISC License\n\nCopyright (c) 2026 Scope\n" {
		t.Errorf("license text = %q, want LF line endings", got)
	}
}

func TestHarvestFailsOnMissingLicense(t *testing.T) {
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "example.com/absent", Version: "v9.9.9", Ecosystem: "go"},
	}}

	_, err := Harvest(doc, testRoots())
	if err == nil {
		t.Fatal("expected an error naming the component with no license file")
	}

	if !strings.Contains(err.Error(), "example.com/absent") {
		t.Errorf("error %q does not name the offending component", err)
	}
}

func TestHarvestSkipsUnknownEcosystems(t *testing.T) {
	// "other" components (pkg:cargo/, pkg:pypi/) have no root to resolve
	// against. They stay on the page as an inventory entry without a text.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "serde", Version: "1.0.0", Ecosystem: "other"},
	}}

	artifact, err := Harvest(doc, testRoots())
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}

	if len(artifact.Components) != 0 {
		t.Errorf("got %+v, want no entries", artifact.Components)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./scripts/licensetexts/ -v`
Expected: FAIL — `undefined: Roots`, `undefined: Harvest`.

- [ ] **Step 5: Implement the harvester**

Create `scripts/licensetexts/harvest.go`:

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/radiergummi/cetacean/internal/api/sbom"
)

// maxTextBytes caps a single license or notice file. Every real one is a few
// kilobytes; anything larger is a misidentified file, and embedding it would
// bloat the binary for no attribution value.
const maxTextBytes = 1 << 20

// licenseNames and noticeNames are the filename globs to search, in order.
// The first match wins, so the more canonical spellings come first.
var (
	licenseNames = []string{"LICENSE*", "LICENCE*", "COPYING*"}
	noticeNames  = []string{"NOTICE*"}
)

// Roots locates the two package stores the harvester reads from. Tests point
// these at fixtures; the CLI points them at the real module cache and
// node_modules.
type Roots struct {
	GoModCache  string
	NodeModules string
}

// Harvest resolves every Go and npm component to its source directory and
// collects its license and notice text into a deduplicated pool.
func Harvest(doc sbom.Document, roots Roots) (sbom.Artifact, error) {
	artifact := sbom.Artifact{
		Texts:      map[string]string{},
		Components: map[string]sbom.ComponentTexts{},
	}

	intern := func(text string) string {
		sum := sha256.Sum256([]byte(text))
		id := hex.EncodeToString(sum[:16])
		artifact.Texts[id] = text

		return id
	}

	for _, component := range doc.Components {
		dir, ok := componentDir(component, roots)
		if !ok {
			// An ecosystem with no package store to read from. It keeps its
			// inventory entry on the page; there is simply no text to attach.
			continue
		}

		license, found, err := readFirst(dir, licenseNames)
		if err != nil {
			return sbom.Artifact{}, err
		}

		if !found {
			return sbom.Artifact{}, fmt.Errorf(
				"no license file for %s %s (looked in %s for %s) — "+
					"add a mapping or vendor the text before shipping it",
				component.Name, component.Version, dir, strings.Join(licenseNames, ", "),
			)
		}

		entry := sbom.ComponentTexts{License: intern(license)}

		notice, hasNotice, err := readFirst(dir, noticeNames)
		if err != nil {
			return sbom.Artifact{}, err
		}

		if hasNotice {
			entry.Notice = intern(notice)
		}

		artifact.Components[sbom.ComponentKey(component)] = entry
	}

	return artifact, nil
}

// componentDir maps a component to the directory its sources were unpacked
// into. Reports false for ecosystems with no local package store.
func componentDir(component sbom.Component, roots Roots) (string, bool) {
	switch component.Ecosystem {
	case "go":
		return filepath.Join(
			roots.GoModCache,
			escapeModulePath(component.Name)+"@"+component.Version,
		), true
	case "npm":
		return filepath.Join(roots.NodeModules, filepath.FromSlash(component.Name)), true
	default:
		return "", false
	}
}

// escapeModulePath applies the Go module cache's case encoding: an uppercase
// letter becomes "!" followed by its lowercase form, so that case-insensitive
// filesystems cannot collide two distinct module paths.
func escapeModulePath(module string) string {
	var out strings.Builder

	for _, r := range module {
		if r >= 'A' && r <= 'Z' {
			out.WriteByte('!')
			out.WriteRune(r + ('a' - 'A'))

			continue
		}

		out.WriteRune(r)
	}

	return out.String()
}

// readFirst returns the contents of the first regular file in dir matching any
// of the patterns, with line endings normalized. Matches are sorted so the
// choice does not depend on directory order.
func readFirst(dir string, patterns []string) (string, bool, error) {
	var matches []string

	for _, pattern := range patterns {
		hits, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return "", false, fmt.Errorf("glob %s in %s: %w", pattern, dir, err)
		}

		for _, hit := range hits {
			info, err := os.Stat(hit)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}

			matches = append(matches, hit)
		}

		if len(matches) > 0 {
			break
		}
	}

	if len(matches) == 0 {
		return "", false, nil
	}

	slices.Sort(matches)
	path := matches[0]

	info, err := os.Stat(path)
	if err != nil {
		return "", false, fmt.Errorf("stat %s: %w", path, err)
	}

	if info.Size() > maxTextBytes {
		return "", false, fmt.Errorf(
			"%s is %d bytes, over the %d-byte cap — it is unlikely to be a license",
			path, info.Size(), maxTextBytes,
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}

	if !utf8.Valid(data) {
		return "", false, fmt.Errorf("%s is not valid UTF-8", path)
	}

	// Normalize CRLF so the artifact hashes the same wherever it is generated.
	return strings.ReplaceAll(string(data), "\r\n", "\n"), true, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./scripts/licensetexts/ -v`
Expected: PASS — all six tests.

- [ ] **Step 7: Verify formatting and linting**

Run: `gofmt -l scripts/ && golangci-lint run ./scripts/...`
Expected: no output from `gofmt`, `0 issues` from the linter.

- [ ] **Step 8: Commit**

```bash
git add scripts/licensetexts/
git commit -m "feat(sbom): harvest dependency license and notice texts"
```

---

### Task 2: Generator CLI and build pipeline

**Files:**
- Create: `scripts/licensetexts/main.go`
- Modify: `scripts/build-sbom.sh` (append a step after the merge)
- Modify: `Makefile:49-51` (`sbom-check`)
- Create: `internal/api/sbom/licensetexts.json` (generated output, committed)

**Interfaces:**
- Consumes: `Harvest`, `Roots`, `Artifact` from Task 1.
- Produces: `go run ./scripts/licensetexts -sbom <path> -out <path>` writing `internal/api/sbom/licensetexts.json`.

- [ ] **Step 1: Write the CLI**

Create `scripts/licensetexts/main.go`:

```go
// Command licensetexts harvests the license and notice text of every
// dependency in the committed CycloneDX SBOM into a content-addressed pool
// embedded alongside it. Run via scripts/build-sbom.sh, not directly.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/sbom"
)

func main() {
	sbomPath := flag.String("sbom", "internal/api/sbom/sbom.cdx.json", "path to the CycloneDX SBOM")
	outPath := flag.String("out", "internal/api/sbom/licensetexts.json", "path to write the artifact")
	nodeModules := flag.String("node-modules", "frontend/node_modules", "path to the npm package store")
	flag.Parse()

	if err := run(*sbomPath, *outPath, *nodeModules); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(sbomPath, outPath, nodeModules string) error {
	raw, err := os.ReadFile(sbomPath)
	if err != nil {
		return fmt.Errorf("read SBOM: %w", err)
	}

	doc, err := sbom.Project(raw)
	if err != nil {
		return fmt.Errorf("project SBOM: %w", err)
	}

	goModCache, err := goEnv("GOMODCACHE")
	if err != nil {
		return err
	}

	artifact, err := Harvest(doc, Roots{GoModCache: goModCache, NodeModules: nodeModules})
	if err != nil {
		return err
	}

	// MarshalIndent sorts map keys, so the committed file is stable across
	// runs; without that the sbom-check gate would fail at random.
	encoded, err := json.MarshalIndent(artifact, "", " ")
	if err != nil {
		return fmt.Errorf("encode artifact: %w", err)
	}

	if err := os.WriteFile(outPath, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	fmt.Printf(
		"wrote %s (%d components, %d distinct texts)\n",
		outPath, len(artifact.Components), len(artifact.Texts),
	)

	return nil
}

func goEnv(name string) (string, error) {
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %w", name, err)
	}

	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("go env %s is empty", name)
	}

	return value, nil
}
```

- [ ] **Step 2: Wire it into the SBOM build**

In `scripts/build-sbom.sh`, after the `jq -s` merge block that writes `$out` and before the final `echo "wrote $out"`, insert:

```bash
echo "==> harvest license + notice texts"
# Reads the merged SBOM we just wrote and resolves each component back to its
# unpacked sources, so it must run after the merge and with node_modules present.
( cd "$repo_root" && go run ./scripts/licensetexts \
    -sbom "$out" \
    -out "$repo_root/internal/api/sbom/licensetexts.json" \
    -node-modules "$repo_root/frontend/node_modules" )
```

- [ ] **Step 3: Extend the freshness gate**

Replace the `sbom-check` target in `Makefile` (currently lines 49-51) with:

```make
sbom-check: sbom
	@git diff --exit-code -- internal/api/sbom/sbom.cdx.json internal/api/sbom/licensetexts.json \
	  || { echo "ERROR: SBOM artifacts are stale. Run 'make sbom' and commit." >&2; exit 1; }
```

- [ ] **Step 4: Generate the artifact**

Run: `make sbom`
Expected: `wrote internal/api/sbom/licensetexts.json (137 components, 85 distinct texts)`. The counts may differ if dependencies changed; what matters is that it does not error.

- [ ] **Step 5: Verify determinism**

```bash
cp internal/api/sbom/licensetexts.json /tmp/first.json
make sbom
diff /tmp/first.json internal/api/sbom/licensetexts.json && echo "deterministic"
```

Expected: `deterministic`, no diff output.

- [ ] **Step 6: Commit**

```bash
git add scripts/licensetexts/main.go scripts/build-sbom.sh Makefile internal/api/sbom/licensetexts.json
git commit -m "build(sbom): generate the license text artifact and gate its freshness"
```

---

### Task 3: Embed the artifact and expose it from the projection

**Files:**
- Create: `internal/api/sbom/texts.go`
- Create: `internal/api/sbom/texts_test.go`
- Modify: `internal/api/sbom/project.go` (the `Component` struct and the `Project` walk)
- Modify: `internal/api/sbom/project_test.go` (add a coverage assertion)

**Interfaces:**
- Consumes: the committed `licensetexts.json` from Task 2; `ComponentKey`'s `ecosystem:name@version` format.
- Produces: `func Text(id string) (string, bool)`, `func Attach(components []Component)`, and `Component.TextID` / `Component.NoticeID` (JSON `textId` / `noticeId`).

- [ ] **Step 1: Write the failing test**

Create `internal/api/sbom/texts_test.go`:

```go
package sbom

import (
	"strings"
	"testing"
)

func TestEveryComponentResolvesToATextInThePool(t *testing.T) {
	// Guards against a half-regenerated pair of artifacts: a projection that
	// references a text id the pool no longer holds would render an empty
	// license dialog in production and nowhere else.
	doc, err := Project(Raw())
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	var missing []string
	for _, component := range doc.Components {
		if component.Ecosystem == "other" {
			continue
		}

		if component.TextID == "" {
			missing = append(missing, component.Name+" (no textId)")

			continue
		}

		if _, ok := Text(component.TextID); !ok {
			missing = append(missing, component.Name+" -> "+component.TextID)
		}

		if component.NoticeID != "" {
			if _, ok := Text(component.NoticeID); !ok {
				missing = append(missing, component.Name+" notice -> "+component.NoticeID)
			}
		}
	}

	if len(missing) > 0 {
		t.Fatalf(
			"%d components do not resolve; run 'make sbom':\n  %s",
			len(missing), strings.Join(missing, "\n  "),
		)
	}
}

func TestTextRejectsUnknownID(t *testing.T) {
	if _, ok := Text("0000000000000000000000000000000000000000"); ok {
		t.Error("unknown id resolved to a text")
	}
}

func TestNoticesArePresentForApacheComponents(t *testing.T) {
	// docker/docker ships a NOTICE, and Apache-2.0 section 4(d) makes carrying
	// it mandatory. If this stops holding, the harvester stopped finding them.
	doc, err := Project(Raw())
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	for _, component := range doc.Components {
		if component.Name == "github.com/docker/docker" {
			if component.NoticeID == "" {
				t.Fatal("github.com/docker/docker has a NOTICE file but no noticeId")
			}

			return
		}
	}

	t.Skip("github.com/docker/docker is no longer a dependency")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/sbom/ -run "TestEveryComponentResolves|TestTextRejects|TestNoticesArePresent" -v`
Expected: FAIL — `component.TextID undefined`, `undefined: Text`.

- [ ] **Step 3: Add the id fields to the projection**

In `internal/api/sbom/project.go`, extend the `Component` struct — add these two fields after `Repository`:

```go
	TextID      string    `json:"textId,omitempty"`
	NoticeID    string    `json:"noticeId,omitempty"`
```

Then, at the end of `Project`, immediately before `return Document{Components: components}, nil`, insert:

```go
	Attach(components)
```

- [ ] **Step 4: Implement the text store**

Create `internal/api/sbom/texts.go`:

```go
package sbom

import (
	_ "embed"
	"encoding/json"
	"log/slog"
)

//go:embed licensetexts.json
var rawTexts []byte

// textArtifact is the decoded licensetexts.json. Its shape is declared in
// artifact.go, shared with the generator that writes it.
var textArtifact Artifact

func init() {
	if err := json.Unmarshal(rawTexts, &textArtifact); err != nil {
		// The licenses page degrades to the identifier-only inventory it
		// served before texts existed rather than taking the process down.
		slog.Warn(
			"sbom: could not decode embedded license texts; "+
				"the licenses page will show no full texts",
			"error", err,
		)
	}

	if textArtifact.Texts == nil {
		textArtifact.Texts = map[string]string{}
	}

	if textArtifact.Components == nil {
		textArtifact.Components = map[string]ComponentTexts{}
	}
}

// Text returns the pooled license or notice text for an id.
func Text(id string) (string, bool) {
	text, ok := textArtifact.Texts[id]

	return text, ok
}

// Attach stamps each component with the ids of its license and notice text.
// Components whose ecosystem has no local package store (and so no harvested
// text) keep their inventory entry with empty ids.
func Attach(components []Component) {
	for i := range components {
		entry, ok := textArtifact.Components[ComponentKey(components[i])]
		if !ok {
			continue
		}

		components[i].TextID = entry.License
		components[i].NoticeID = entry.Notice
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/api/sbom/ -v`
Expected: PASS — including the pre-existing projection tests.

- [ ] **Step 6: Commit**

```bash
git add internal/api/sbom/
git commit -m "feat(sbom): embed license texts and attach their ids to the projection"
```

---

### Task 4: License text endpoint

**Files:**
- Modify: `internal/api/licenses.go`
- Modify: `internal/api/router.go:136-139`
- Modify: `internal/api/licenses_test.go`
- Modify: `api/openapi.yaml` (the `/-/licenses` path section)

**Interfaces:**
- Consumes: `sbom.Text(id)` from Task 3; `sbom.Notices()` from Task 5 is **not** required here — `/-/notices` is added in Task 5 once the document generator exists.
- Produces: `GET /-/licenses/texts/{id}` returning `text/plain; charset=utf-8`.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/licenses_test.go`:

```go
func TestHandleLicenseText(t *testing.T) {
	doc, err := sbom.Project(sbom.Raw())
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	var id string
	for _, component := range doc.Components {
		if component.TextID != "" {
			id = component.TextID

			break
		}
	}

	if id == "" {
		t.Fatal("no component carries a text id")
	}

	t.Run("serves the text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/-/licenses/texts/"+id, nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()

		HandleLicenseText(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("Content-Type = %q", got)
		}

		if rec.Body.Len() == 0 {
			t.Error("empty body")
		}

		// Content-addressed ids never point at different bytes, so the
		// response is safe to cache forever.
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
			t.Errorf("Cache-Control = %q, want it to mark the response immutable", got)
		}
	})

	t.Run("304s a matching ETag", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/-/licenses/texts/"+id, nil)
		req.SetPathValue("id", id)
		first := httptest.NewRecorder()
		HandleLicenseText(first, req)

		conditional := httptest.NewRequest(http.MethodGet, "/-/licenses/texts/"+id, nil)
		conditional.SetPathValue("id", id)
		conditional.Header().Set("If-None-Match", first.Header().Get("ETag"))
		second := httptest.NewRecorder()

		HandleLicenseText(second, conditional)

		if second.Code != http.StatusNotModified {
			t.Errorf("status = %d, want 304", second.Code)
		}
	})

	t.Run("404s an unknown id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/-/licenses/texts/deadbeef", nil)
		req.SetPathValue("id", "deadbeef")
		rec := httptest.NewRecorder()

		HandleLicenseText(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})
}
```

Add `"strings"` to that file's imports if it is not already present.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestHandleLicenseText -v`
Expected: FAIL — `undefined: HandleLicenseText`.

- [ ] **Step 3: Implement the handler**

In `internal/api/licenses.go`, add after `HandleLicenses`:

```go
// HandleLicenseText serves one pooled license or notice text by its
// content-addressed id. Public (registered under the auth-exempt /-/ prefix).
func HandleLicenseText(w http.ResponseWriter, r *http.Request) {
	text, ok := sbom.Text(r.PathValue("id"))
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "no license text with that identifier")

		return
	}

	// The id is a hash of the bytes, so a given URL's content can never
	// change. Nothing to revalidate.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writeRawWithETag(w, r, []byte(text))
}
```

Note `writeRawWithETag` sets `Cache-Control: no-cache`; setting it *after* the call would be too late because the body is already written, so it must be set before — which the code above does, and `writeRawWithETag` then overwrites it. To keep the immutable value, change `writeRawWithETag` in `internal/api/etag.go:51-63` to only set the default when the caller has not:

```go
func writeRawWithETag(w http.ResponseWriter, r *http.Request, data []byte) {
	etag := computeETag(data)
	w.Header().Set("ETag", etag)

	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache")
	}
	...
```

- [ ] **Step 4: Register the route**

In `internal/api/router.go`, beside the existing `GET /-/licenses` registration on line 136:

```go
	mux.HandleFunc("GET /-/licenses/texts/{id}", HandleLicenseText)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/api/ -run TestHandleLicense -v`
Expected: PASS, including the pre-existing `/-/licenses` tests (the added `textId` field must not break them).

- [ ] **Step 6: Document the endpoint**

In `api/openapi.yaml`, beside the existing `/-/licenses` path, add:

```yaml
  /-/licenses/texts/{id}:
    get:
      tags: [Meta]
      summary: Full text of one dependency's license or notice
      description: >-
        Returns the verbatim license or NOTICE text identified by `id`, which
        comes from a component's `textId` or `noticeId` on `/-/licenses`. Ids
        are content hashes, so a response is immutable and cacheable
        indefinitely. Public — no authentication required.
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
          description: A `textId` or `noticeId` from the licenses document.
      responses:
        "200":
          description: The license or notice text
          content:
            text/plain:
              schema: { type: string }
        "304": { description: Not modified }
        "404":
          description: No text with that identifier
          content:
            application/problem+json:
              schema: { $ref: "#/components/schemas/Problem" }
```

Confirm the `Problem` schema reference name by grepping: `grep -n "Problem:" api/openapi.yaml`. Use whatever name that returns.

- [ ] **Step 7: Run the full backend suite**

Run: `go test ./... && golangci-lint run ./...`
Expected: all packages `ok`, `0 issues`. The OpenAPI exhaustiveness test in `internal/api/openapi_exhaustive_test.go` verifies every route is documented, so a missing spec entry fails here.

- [ ] **Step 8: Commit**

```bash
git add internal/api/ api/openapi.yaml
git commit -m "feat(api): serve individual dependency license texts"
```

---

### Task 5: Generated THIRD_PARTY_LICENSES and the notices endpoint

**Files:**
- Create: `THIRD_PARTY_LICENSES.preamble`
- Modify: `THIRD_PARTY_LICENSES` (becomes generated output)
- Create: `internal/api/sbom/notices.go`
- Create: `internal/api/sbom/notices.txt` (generated, committed)
- Create: `internal/api/sbom/notices_test.go`
- Modify: `scripts/licensetexts/main.go` (also write the notices document)
- Modify: `internal/api/licenses.go`, `internal/api/router.go`, `api/openapi.yaml`

**Interfaces:**
- Consumes: `Artifact` and `ComponentKey` from Task 1; `sbom.Text` from Task 3.
- Produces: `func sbom.Notices() string` returning the full attribution document, and `GET /-/notices`.

- [ ] **Step 1: Split the curated preamble out**

```bash
git mv THIRD_PARTY_LICENSES THIRD_PARTY_LICENSES.preamble
```

Then edit `THIRD_PARTY_LICENSES.preamble`: keep the existing Lucide/Feather section verbatim, and replace the opening paragraph (which currently disclaims covering transitive dependencies) with:

```
Third-Party Licenses
====================

Cetacean bundles or derives from third-party components whose licenses require
their copyright and permission notices to be reproduced in distributions.

This file has two parts. The notes below are curated: they cover material that a
dependency scan cannot see, and licensing choices that a bare license text would
leave ambiguous. Everything after the "Bundled dependencies" divider is generated
from the committed software bill of materials by `make sbom`; do not edit it by
hand.


Licensing notes
---------------

elkjs is offered under "EPL-2.0 OR GPL-3.0-or-later". Cetacean elects
GPL-3.0-or-later, which matches Cetacean's own license. The full text of
GPL-3.0-or-later is in the LICENSE file at the root of this repository.

@fontsource-variable/geist is redistributed unmodified under SIL OFL-1.1. The
Reserved Font Name provision restricts modified versions only, and Cetacean
modifies nothing, so no renaming applies.

github.com/opencontainers/go-digest carries two licenses: Apache-2.0 for its
code and CC-BY-SA-4.0 for its specification text. Cetacean redistributes both
unmodified, and reproduces the Apache-2.0 text below.
```

Keep the Lucide/Feather section after those notes.

- [ ] **Step 2: Write the failing test**

Create `internal/api/sbom/notices_test.go`:

```go
package sbom

import (
	"os"
	"strings"
	"testing"
)

func TestNoticesMatchesTheCommittedFile(t *testing.T) {
	// The repo file and the served document are the same artifact. If they
	// drift, one of the two audiences is reading stale attribution.
	committed, err := os.ReadFile("../../../THIRD_PARTY_LICENSES")
	if err != nil {
		t.Fatalf("read THIRD_PARTY_LICENSES: %v", err)
	}

	if Notices() != string(committed) {
		t.Error("Notices() differs from THIRD_PARTY_LICENSES; run 'make sbom'")
	}
}

func TestNoticesCoversEveryComponent(t *testing.T) {
	doc, err := Project(Raw())
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	notices := Notices()

	var absent []string
	for _, component := range doc.Components {
		if component.TextID == "" {
			continue
		}

		if !strings.Contains(notices, component.Name) {
			absent = append(absent, component.Name)
		}
	}

	if len(absent) > 0 {
		t.Errorf("%d components absent from the notices: %v", len(absent), absent)
	}
}

func TestNoticesCarriesTheCuratedPreamble(t *testing.T) {
	notices := Notices()

	for _, want := range []string{"elkjs", "Reserved Font Name", "Lucide"} {
		if !strings.Contains(notices, want) {
			t.Errorf("notices do not mention %q — the curated preamble was dropped", want)
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/api/sbom/ -run TestNotices -v`
Expected: FAIL — `undefined: Notices`.

- [ ] **Step 4: Embed the generated document**

`//go:embed` cannot reach outside its own package directory — a `..` in the pattern is a compile error — so the generator writes the document twice, to the repo root for anyone browsing the source and to a copy inside the package for embedding. `TestNoticesMatchesTheCommittedFile` is what keeps the two honest.

Create `internal/api/sbom/notices.go`:

```go
package sbom

import _ "embed"

// notices.txt is a byte-identical copy of THIRD_PARTY_LICENSES at the repo
// root, written by 'make sbom'. The duplicate exists because go:embed cannot
// reference a parent directory.
//
//go:embed notices.txt
var notices string

// Notices returns the full third-party attribution document: the curated
// preamble followed by every bundled dependency's license and notice text.
// Served at GET /-/notices.
func Notices() string { return notices }
```

This file will not compile until Step 5 generates `notices.txt`; that is expected, and Step 5 immediately follows.

- [ ] **Step 5: Generate the document**

In `scripts/licensetexts/main.go`, add a `renderNotices` function and call it from `run` after writing the artifact:

```go
// renderNotices builds the attribution document: the curated preamble, then
// one section per component with its license and any NOTICE it ships.
func renderNotices(doc sbom.Document, artifact sbom.Artifact, preamble string) string {
	var out strings.Builder

	out.WriteString(preamble)
	out.WriteString("\n\nBundled dependencies\n")
	out.WriteString("====================\n\n")
	out.WriteString(
		"Generated from the committed software bill of materials. Do not edit by hand.\n\n",
	)

	for _, component := range doc.Components {
		entry, ok := artifact.Components[sbom.ComponentKey(component)]
		if !ok {
			continue
		}

		title := component.Name
		if component.Version != "" {
			title += " " + component.Version
		}

		out.WriteString("\n" + strings.Repeat("-", 78) + "\n\n")
		out.WriteString(title + "\n")

		if component.Homepage != "" {
			out.WriteString(component.Homepage + "\n")
		} else if component.Repository != "" {
			out.WriteString(component.Repository + "\n")
		}

		var ids []string
		for _, license := range component.Licenses {
			if id := license.ID; id != "" {
				ids = append(ids, id)
			} else if license.Name != "" {
				ids = append(ids, license.Name)
			}
		}

		if len(ids) > 0 {
			out.WriteString("License: " + strings.Join(ids, ", ") + "\n")
		}

		out.WriteString("\n" + artifact.Texts[entry.License] + "\n")

		if entry.Notice != "" {
			out.WriteString("\nNOTICE:\n\n" + artifact.Texts[entry.Notice] + "\n")
		}
	}

	return out.String()
}
```

Wire it into `run`, after the `os.WriteFile(outPath, ...)` call:

```go
	preamble, err := os.ReadFile("THIRD_PARTY_LICENSES.preamble")
	if err != nil {
		return fmt.Errorf("read preamble: %w", err)
	}

	rendered := renderNotices(doc, artifact, strings.TrimRight(string(preamble), "\n"))

	// Both audiences read the same bytes: the repo file for anyone browsing
	// the source, the embedded copy for GET /-/notices.
	for _, path := range []string{"THIRD_PARTY_LICENSES", "internal/api/sbom/notices.txt"} {
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
```

Both paths are already what `notices.go` from Step 4 expects.

- [ ] **Step 6: Add the notices endpoint**

In `internal/api/licenses.go`:

```go
// HandleNotices serves the full third-party attribution document. Public
// (registered under the auth-exempt /-/ prefix).
func HandleNotices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writeRawWithETag(w, r, []byte(sbom.Notices()))
}
```

In `internal/api/router.go`, beside the other licenses routes:

```go
	mux.HandleFunc("GET /-/notices", HandleNotices)
```

Add the corresponding `/-/notices` path to `api/openapi.yaml`, mirroring the `/-/licenses/texts/{id}` entry from Task 4 but with no parameters and no 404 response.

- [ ] **Step 7: Generate and verify**

```bash
make sbom
go test ./internal/api/... -v -run "TestNotices|TestHandleNotices"
```

Expected: PASS. Then eyeball the result — this is a legal document, and a test can only tell you it is non-empty:

```bash
head -60 THIRD_PARTY_LICENSES
grep -c "^----" THIRD_PARTY_LICENSES   # one divider per component
grep -A2 "^NOTICE:" THIRD_PARTY_LICENSES | head -20
```

Expected: the curated preamble first, roughly 137 dividers, and the eight NOTICE blocks rendering as their own labelled sections.

- [ ] **Step 8: Add notices.txt to the freshness gate**

Extend the `sbom-check` target's `git diff` file list with `THIRD_PARTY_LICENSES` and `internal/api/sbom/notices.txt`.

- [ ] **Step 9: Commit**

```bash
git add THIRD_PARTY_LICENSES THIRD_PARTY_LICENSES.preamble internal/api/sbom/ scripts/licensetexts/main.go internal/api/ api/openapi.yaml Makefile
git commit -m "feat(sbom): generate the third-party attribution document"
```

---

### Task 6: License filter on the licenses page

**Files:**
- Modify: `frontend/src/pages/Licenses.tsx`
- Modify: `frontend/src/pages/Licenses.test.tsx`

**Interfaces:**
- Consumes: `LicenseComponent.licenses[].id` (already present).
- Produces: nothing other tasks depend on.

- [ ] **Step 1: Write the failing test**

Append to `frontend/src/pages/Licenses.test.tsx`, matching the file's existing render/mock setup:

```tsx
it("filters the grid by license", async () => {
  renderLicensesPage({
    components: [
      { name: "alpha", ecosystem: "npm", licenses: [{ id: "MIT" }] },
      { name: "beta", ecosystem: "npm", licenses: [{ id: "Apache-2.0" }] },
    ],
  });

  await screen.findByText("alpha");

  await userEvent.click(screen.getByRole("button", { name: /all licenses/i }));
  await userEvent.click(screen.getByRole("option", { name: /Apache-2\.0/ }));

  expect(screen.queryByText("alpha")).not.toBeInTheDocument();
  expect(screen.getByText("beta")).toBeInTheDocument();
});

it("combines the license filter with search", async () => {
  renderLicensesPage({
    components: [
      { name: "alpha", ecosystem: "npm", licenses: [{ id: "MIT" }] },
      { name: "alpine", ecosystem: "npm", licenses: [{ id: "MIT" }] },
      { name: "beta", ecosystem: "npm", licenses: [{ id: "MIT" }] },
    ],
  });

  await screen.findByText("alpha");

  await userEvent.click(screen.getByRole("button", { name: /all licenses/i }));
  await userEvent.click(screen.getByRole("option", { name: "MIT" }));
  await userEvent.type(screen.getByPlaceholderText(/search by name/i), "alp");

  await waitFor(() => expect(screen.queryByText("beta")).not.toBeInTheDocument());
  expect(screen.getByText("alpha")).toBeInTheDocument();
  expect(screen.getByText("alpine")).toBeInTheDocument();
});
```

Read the existing `Licenses.test.tsx` first and reuse its helper for rendering with mocked data rather than inventing `renderLicensesPage` if a differently-named one already exists.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run src/pages/Licenses.test.tsx`
Expected: FAIL — no element matching `/all licenses/i`.

- [ ] **Step 3: Implement the filter**

In `frontend/src/pages/Licenses.tsx`, add the import:

```tsx
import { Combobox } from "@/components/ui/combobox";
```

Add state beside the existing `ecosystem` state:

```tsx
const [license, setLicense] = useState("all");
```

Derive the option list from the data, after the existing `counts` memo:

```tsx
const licenseOptions = useMemo(() => {
  const counts = new Map<string, number>();

  for (const component of components) {
    for (const entry of component.licenses) {
      const id = entry.id || entry.name || "Unknown";

      counts.set(id, (counts.get(id) ?? 0) + 1);
    }
  }

  const sorted = [...counts.entries()].sort(([a], [b]) => a.localeCompare(b));

  return [
    { value: "all", label: "All licenses" },
    ...sorted.map(([id, count]) => ({ value: id, label: `${id} (${count})` })),
  ];
}, [components]);
```

Extend the `filtered` memo's predicate — insert before the `needle` check:

```tsx
if (
  license !== "all" &&
  !component.licenses.some((entry) => (entry.id || entry.name || "Unknown") === license)
) {
  return false;
}
```

Add `license` to that memo's dependency array. Render the control inside the existing toolbar `div`, before the ecosystem buttons:

```tsx
<Combobox
  value={license}
  onChange={setLicense}
  options={licenseOptions}
  allowCustom={false}
  className="sm:max-w-56"
/>
```

`Combobox` rather than `Select`: it is the dropdown this codebase already uses (`RuntimeEditor`, `NetworksEditor`, `ConfigsEditor`), `ui/select.tsx` has no consumers yet, and the license list is long enough that its type-to-filter behaviour earns its place.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd frontend && npx vitest run src/pages/Licenses.test.tsx`
Expected: PASS.

- [ ] **Step 5: Verify types, lint, and formatting**

Run: `cd frontend && npx tsc -b --noEmit && npx oxlint src && npx oxfmt --check src`
Expected: no errors; no files listed by `oxfmt`.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/Licenses.tsx frontend/src/pages/Licenses.test.tsx
git commit -m "feat(frontend): filter the licenses page by license"
```

---

### Task 7: License text dialog

**Files:**
- Create: `frontend/src/components/LicenseTextDialog.tsx`
- Create: `frontend/src/components/LicenseTextDialog.test.tsx`
- Modify: `frontend/src/api/client.ts` (add `licenseText`)
- Modify: `frontend/src/api/types.ts` (`LicenseComponent` gains the two ids)
- Modify: `frontend/src/pages/Licenses.tsx` (badge becomes a trigger)

**Interfaces:**
- Consumes: `GET /-/licenses/texts/{id}` from Task 4; `textId`/`noticeId` from Task 3.
- Produces: `api.licenseText(id, signal?): Promise<string>` and `<LicenseTextDialog component={…} open={…} onOpenChange={…} />`.

- [ ] **Step 1: Extend the types and client**

In `frontend/src/api/types.ts`, add to `LicenseComponent`:

```ts
  textId?: string | undefined;
  noticeId?: string | undefined;
```

In `frontend/src/api/client.ts`, beside the existing `licenses` method:

```ts
  licenseText: async (id: string, signal?: AbortSignal): Promise<string> => {
    const response = await fetch(apiPath(`/-/licenses/texts/${encodeURIComponent(id)}`), {
      signal: composeSignals(signal, AbortSignal.timeout(defaultTimeoutMilliseconds)),
    });

    if (!response.ok) {
      await throwResponseError(response);
    }

    return await response.text();
  },
```

- [ ] **Step 2: Write the failing test**

Create `frontend/src/components/LicenseTextDialog.test.tsx`:

```tsx
import LicenseTextDialog from "./LicenseTextDialog";
import { api } from "@/api/client";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

const component = {
  name: "@floating-ui/core",
  ecosystem: "npm",
  licenses: [{ id: "MIT" }],
  textId: "aaaa",
  noticeId: "bbbb",
};

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("LicenseTextDialog", () => {
  it("fetches nothing until it is opened", () => {
    const licenseText = vi.spyOn(api, "licenseText").mockResolvedValue("MIT License");

    render(
      <LicenseTextDialog
        component={component}
        open={false}
        onOpenChange={() => {}}
      />,
    );

    expect(licenseText).not.toHaveBeenCalled();
  });

  it("renders the license and the notice as separate sections", async () => {
    vi.spyOn(api, "licenseText").mockImplementation(async (id: string) =>
      id === "aaaa" ? "MIT License body" : "NOTICE body",
    );

    render(
      <LicenseTextDialog
        component={component}
        open
        onOpenChange={() => {}}
      />,
    );

    await waitFor(() => expect(screen.getByText("MIT License body")).toBeInTheDocument());

    // The distinction is legal, not cosmetic: a NOTICE is not part of the
    // license text and must not read as though it were.
    expect(screen.getByText("NOTICE body")).toBeInTheDocument();
    expect(screen.getByText(/^NOTICE$/)).toBeInTheDocument();
  });

  it("reports a failed fetch instead of showing an empty dialog", async () => {
    vi.spyOn(api, "licenseText").mockRejectedValue(new Error("nope"));

    render(
      <LicenseTextDialog
        component={{ ...component, noticeId: undefined }}
        open
        onOpenChange={() => {}}
      />,
    );

    await waitFor(() => expect(screen.getByText(/could not load/i)).toBeInTheDocument());
  });
});
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd frontend && npx vitest run src/components/LicenseTextDialog.test.tsx`
Expected: FAIL — cannot resolve `./LicenseTextDialog`.

- [ ] **Step 4: Implement the dialog**

Create `frontend/src/components/LicenseTextDialog.tsx`:

```tsx
import { api } from "@/api/client";
import type { LicenseComponent } from "@/api/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

interface LicenseTextDialogProps {
  component: LicenseComponent;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * Shows one dependency's verbatim license text, and its NOTICE when it ships
 * one. Texts are fetched on open rather than with the page: the pool is some
 * 300KB and most visitors open none of it.
 */
export default function LicenseTextDialog({
  component,
  open,
  onOpenChange,
}: LicenseTextDialogProps) {
  const { textId, noticeId } = component;

  const license = useQuery({
    queryKey: ["licenseText", textId],
    queryFn: ({ signal }) => api.licenseText(textId!, signal),
    enabled: open && !!textId,
    staleTime: Infinity,
  });

  const notice = useQuery({
    queryKey: ["licenseText", noticeId],
    queryFn: ({ signal }) => api.licenseText(noticeId!, signal),
    enabled: open && !!noticeId,
    staleTime: Infinity,
  });

  const identifiers = useMemo(
    () =>
      component.licenses
        .map(({ id, name }) => id || name)
        .filter(Boolean)
        .join(", "),
    [component.licenses],
  );

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
    >
      <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{component.name}</DialogTitle>
          <DialogDescription>
            {identifiers}
            {component.version ? ` · ${component.version}` : ""}
          </DialogDescription>
        </DialogHeader>

        {license.isError ? (
          <p className="text-sm text-destructive">Could not load the license text.</p>
        ) : (
          <pre className="font-mono text-xs whitespace-pre-wrap text-muted-foreground">
            {license.data ?? "Loading…"}
          </pre>
        )}

        {noticeId && (
          <section className="border-t pt-4">
            <h3 className="mb-2 text-sm font-medium">NOTICE</h3>
            {notice.isError ? (
              <p className="text-sm text-destructive">Could not load the notice.</p>
            ) : (
              <pre className="font-mono text-xs whitespace-pre-wrap text-muted-foreground">
                {notice.data ?? "Loading…"}
              </pre>
            )}
          </section>
        )}
      </DialogContent>
    </Dialog>
  );
}
```

`staleTime: Infinity` because the ids are content hashes — the bytes behind one never change, so refetching is pure waste.

- [ ] **Step 5: Wire it into the cards**

In `frontend/src/pages/Licenses.tsx`'s `LicenseCard`, add state and make the badge a trigger:

```tsx
const [showText, setShowText] = useState(false);
```

Replace the licenses `.map(...)` badge block with:

```tsx
{component.licenses.map((license, index) => (
  <button
    key={license.id || license.name || String(index)}
    type="button"
    onClick={() => setShowText(true)}
    disabled={!component.textId}
    className="cursor-pointer disabled:cursor-default"
  >
    <Badge variant="secondary">{license.id || license.name || "Unknown"}</Badge>
  </button>
))}
```

And render the dialog at the end of the `Card`:

```tsx
{component.textId && (
  <LicenseTextDialog
    component={component}
    open={showText}
    onOpenChange={setShowText}
  />
)}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd frontend && npx vitest run src/components/LicenseTextDialog.test.tsx src/pages/Licenses.test.tsx`
Expected: PASS.

- [ ] **Step 7: Verify types, lint, and formatting**

Run: `cd frontend && npx tsc -b --noEmit && npx oxlint src && npx oxfmt --check src`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/components/LicenseTextDialog.tsx frontend/src/components/LicenseTextDialog.test.tsx frontend/src/api/ frontend/src/pages/Licenses.tsx
git commit -m "feat(frontend): show full license and notice text on the licenses page"
```

---

### Task 8: Notices link, documentation, and changelog

**Files:**
- Modify: `frontend/src/pages/Licenses.tsx` (header action)
- Modify: `docs/api.md` (meta endpoints section)
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: `GET /-/notices` from Task 5.
- Produces: nothing.

- [ ] **Step 1: Add the download link**

In `frontend/src/pages/Licenses.tsx`, pass an action to the existing `PageHeader`:

```tsx
<PageHeader
  title="Open-source licenses"
  subtitle="Every open-source project bundled into Cetacean."
  actions={
    <a
      href={apiPath("/-/notices")}
      download="THIRD_PARTY_LICENSES.txt"
      className={buttonVariants({ variant: "outline", size: "sm" })}
    >
      Download all notices
    </a>
  }
/>
```

Import `apiPath` from `@/api/client` and `buttonVariants` from `@/components/ui/button`. Check that `apiPath` is exported — if it is not, export it, since the link must respect `CETACEAN_BASE_PATH`.

- [ ] **Step 2: Verify it resolves under a base path**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run src/pages/Licenses.test.tsx`
Expected: PASS. Then confirm by inspection that the href is built through `apiPath` and not hardcoded to `/-/notices` — a hardcoded path silently 404s for every deployment behind `CETACEAN_BASE_PATH`.

- [ ] **Step 3: Document the endpoints**

In `docs/api.md`, in the section listing meta endpoints alongside `/-/licenses`, add:

```markdown
| `GET /-/licenses/texts/{id}` | Verbatim license or NOTICE text for one dependency. Ids come from a component's `textId`/`noticeId` on `/-/licenses`; they are content hashes, so responses are immutable. |
| `GET /-/notices` | The complete third-party attribution document — every bundled dependency's license and NOTICE. Identical to `THIRD_PARTY_LICENSES` in the source tree. |
```

Match the surrounding table's column layout; if that section is prose rather than a table, follow the prose style instead.

- [ ] **Step 4: Add the changelog entry**

Under `## [Unreleased]`, in an `### Added` section (create it above `### Fixed` if absent):

```markdown
- The licenses page now carries the full license text of every bundled dependency, along with the NOTICE files that Apache-2.0 projects require be passed on, and can be filtered by license. A complete attribution document is downloadable from the page and available at `/-/notices`
```

- [ ] **Step 5: Full verification**

Run: `make check && make sbom-check`
Expected: both exit 0. `make check` runs golangci-lint, oxlint, formatting checks, and the full Go suite; `make sbom-check` proves the committed artifacts are current.

Then run the frontend suite from the right working directory — running `vitest` from the repo root silently loses the jsdom environment and fails hundreds of tests with `document is not defined`:

```bash
cd frontend && npx vitest run
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/Licenses.tsx docs/api.md CHANGELOG.md
git commit -m "feat: link the full attribution document from the licenses page"
```

---

## Verification checklist

Run after Task 8, before opening a PR:

- [ ] `make check` exits 0
- [ ] `make sbom-check` exits 0 on a clean tree
- [ ] `cd frontend && npx vitest run` — all tests pass
- [ ] `THIRD_PARTY_LICENSES` opens with the curated preamble and contains ~137 dependency sections
- [ ] The eight `NOTICE`-shipping dependencies (`docker/docker`, `cyclonedx-go`, `go-oidc/v3`, `yaml.v3`, and four `prometheus/*`) each render a NOTICE block
- [ ] `curl -s localhost:9000/-/licenses | jq '.components[0]'` shows `textId`
- [ ] `curl -sI localhost:9000/-/licenses/texts/<id>` returns `text/plain` and an immutable `Cache-Control`
- [ ] The licenses page filters by license, and opening a badge shows the text without having fetched it on page load (verify in the browser's network panel)
