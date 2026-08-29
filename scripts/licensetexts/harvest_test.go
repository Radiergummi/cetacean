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
