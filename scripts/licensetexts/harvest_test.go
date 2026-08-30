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

func TestHarvestMatchesLowercaseLicenseFilename(t *testing.T) {
	// npm packages commonly ship a lowercase "license" file (clsx,
	// next-themes, the sindresorhus family, ...); the match must not depend
	// on case.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "lowercased", Version: "1.0.0", Ecosystem: "npm"},
	}}

	artifact, err := Harvest(doc, testRoots())
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}

	entry, ok := artifact.Components["npm:lowercased@1.0.0"]
	if !ok {
		t.Fatalf("component missing from artifact: %+v", artifact.Components)
	}

	if got := artifact.Texts[entry.License]; got != "MIT License\n\nCopyright (c) 2026 Lowercased\n" {
		t.Errorf("license text = %q", got)
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

func TestHarvestSkipsSourceLikeStemMatches(t *testing.T) {
	// tailscale.com@v1.102.3 ships both LICENSE and license_test.go; the
	// stem match must not embed the Go source file. "License.go" sorts
	// before "license.txt" in ASCII order (uppercase 'L' < lowercase 'l'),
	// so without the extension check the wrong file would win here.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "example.com/sourcelike", Version: "v1.0.0", Ecosystem: "go"},
	}}

	artifact, err := Harvest(doc, testRoots())
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}

	entry, ok := artifact.Components["go:example.com/sourcelike@v1.0.0"]
	if !ok {
		t.Fatalf("component missing from artifact: %+v", artifact.Components)
	}

	if got := artifact.Texts[entry.License]; got != "MIT License\n\nCopyright (c) 2026 Source Like\n" {
		t.Errorf("license text = %q, want the real license file, not the source lookalike", got)
	}
}

func TestHarvestFailsWhenOnlyMatchIsSourceLike(t *testing.T) {
	// A package shipping only a "license_test.go"-style file with no real
	// license must fail loudly, not silently embed the source file.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "example.com/sourceonly", Version: "v1.0.0", Ecosystem: "go"},
	}}

	_, err := Harvest(doc, testRoots())
	if err == nil {
		t.Fatal("expected an error naming the component with no license file")
	}

	if !strings.Contains(err.Error(), "example.com/sourceonly") {
		t.Errorf("error %q does not name the offending component", err)
	}
}

func TestHarvestResolvesNpmVersionsSeparately(t *testing.T) {
	// npm hoists one version of a package to the top of node_modules and nests
	// the rest under their dependents. Resolving by name alone would hand both
	// SBOM entries the hoisted version's license text.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "duplicated", Version: "2.0.0", Ecosystem: "npm"},
		{Name: "duplicated", Version: "1.0.0", Ecosystem: "npm"},
	}}

	artifact, err := Harvest(doc, testRoots())
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}

	hoisted := artifact.Components["npm:duplicated@2.0.0"]
	if got := artifact.Texts[hoisted.License]; got != "MIT License\n\nCopyright (c) 2026 Duplicated Two\n" {
		t.Errorf("hoisted license text = %q", got)
	}

	nested := artifact.Components["npm:duplicated@1.0.0"]
	if got := artifact.Texts[nested.License]; got != "MIT License\n\nCopyright (c) 2026 Duplicated One\n" {
		t.Errorf("nested license text = %q, want the nested copy's own text", got)
	}
}

func TestHarvestFailsOnUninstalledNpmVersion(t *testing.T) {
	// Silently falling back to whichever version happens to be installed would
	// put the wrong license text in the attribution document.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "duplicated", Version: "9.9.9", Ecosystem: "npm"},
	}}

	_, err := Harvest(doc, testRoots())
	if err == nil {
		t.Fatal("expected an error naming the version that is not installed")
	}

	if !strings.Contains(err.Error(), "duplicated@9.9.9") {
		t.Errorf("error %q does not name the offending component", err)
	}
}

func TestHarvestCollectsEveryLicenseOfADualLicensedPackage(t *testing.T) {
	// A package offering Apache-2.0 or MIT ships both texts and no plain
	// LICENSE. Embedding only the first would drop half the attribution.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "example.com/dual", Version: "v1.0.0", Ecosystem: "go"},
	}}

	artifact, err := Harvest(doc, testRoots())
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}

	text := artifact.Texts[artifact.Components["go:example.com/dual@v1.0.0"].License]

	for _, want := range []string{"LICENSE-APACHE", "Apache License 2.0", "LICENSE-MIT", "MIT License"} {
		if !strings.Contains(text, want) {
			t.Errorf("combined text %q is missing %q", text, want)
		}
	}
}

func TestHarvestCollapsesDuplicateLicenseFiles(t *testing.T) {
	// LICENSE and LICENSE.md holding the same bytes is one obligation, not
	// two, and printing it twice only pads the attribution document.
	doc := sbom.Document{Components: []sbom.Component{
		{Name: "example.com/twinned", Version: "v1.0.0", Ecosystem: "go"},
	}}

	artifact, err := Harvest(doc, testRoots())
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}

	text := artifact.Texts[artifact.Components["go:example.com/twinned@v1.0.0"].License]
	if text != "MIT License\n\nCopyright (c) 2026 Twinned\n" {
		t.Errorf("text = %q, want the license once and unlabelled", text)
	}
}
