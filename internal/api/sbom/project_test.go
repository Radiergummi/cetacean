package sbom

import (
	"os"
	"strings"
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
		t.Fatalf(
			"got %d components, want 2 (container without purl must be skipped)",
			len(doc.Components),
		)
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

func TestProjectDeduplicates(t *testing.T) {
	raw, err := os.ReadFile("testdata/example.cdx.json")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := Project(raw)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	// The fixture contains two identical react@19.0.0 entries; only one should appear.
	count := 0
	for _, component := range doc.Components {
		if component.Name == "react" && component.Version == "19.0.0" {
			count++
		}
	}

	if count != 1 {
		t.Fatalf("react@19.0.0 appears %d times, want exactly 1", count)
	}
}

func TestProjectOtherEcosystemAndCrossEcosystemDedup(t *testing.T) {
	// A cargo component must be bucketed as "other" (not dropped); a Go module
	// and an npm package that share the same name@version must both survive
	// (dedup is ecosystem-scoped); the purl-less container must be skipped.
	raw := []byte(`{
		"bomFormat": "CycloneDX",
		"specVersion": "1.6",
		"components": [
			{"name": "serde", "version": "1.0.0", "purl": "pkg:cargo/serde@1.0.0"},
			{"name": "shared", "version": "1.0.0", "purl": "pkg:golang/example.com/shared@1.0.0"},
			{"name": "shared", "version": "1.0.0", "purl": "pkg:npm/shared@1.0.0"},
			{"name": "no-purl-container"}
		]
	}`)

	doc, err := Project(raw)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	byEcosystem := map[string]int{}
	shared := 0
	for _, component := range doc.Components {
		byEcosystem[component.Ecosystem]++
		if component.Name == "shared" && component.Version == "1.0.0" {
			shared++
		}
	}

	if byEcosystem["other"] != 1 {
		t.Errorf(
			"cargo component: ecosystem \"other\" count = %d, want 1 (%+v)",
			byEcosystem["other"],
			doc.Components,
		)
	}

	if shared != 2 {
		t.Errorf("shared@1.0.0 across go+npm: got %d, want 2 (ecosystem-scoped dedup)", shared)
	}

	if len(doc.Components) != 3 {
		t.Errorf(
			"got %d components, want 3 (cargo + go-shared + npm-shared; container skipped)",
			len(doc.Components),
		)
	}
}

func TestProjectInvalidJSON(t *testing.T) {
	if _, err := Project([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestProjectQualifiesScopedNames(t *testing.T) {
	// CycloneDX splits a scoped npm package into group ("@floating-ui") and
	// name ("core"). The projection must rejoin them, or the licenses page
	// renders the scope-less tail and two distinct packages can collide on a
	// single name.
	raw := []byte(`{
		"bomFormat": "CycloneDX",
		"specVersion": "1.6",
		"components": [
			{
				"group": "@floating-ui",
				"name": "core",
				"version": "1.7.4",
				"purl": "pkg:npm/%40floating-ui/core@1.7.4"
			},
			{
				"group": "@base-ui",
				"name": "react",
				"version": "1.7.0",
				"purl": "pkg:npm/%40base-ui/react@1.7.0"
			},
			{"name": "react", "version": "19.0.0", "purl": "pkg:npm/react@19.0.0"}
		]
	}`)

	doc, err := Project(raw)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	var names []string
	for _, component := range doc.Components {
		names = append(names, component.Name)
	}

	want := []string{"@base-ui/react", "@floating-ui/core", "react"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}

	for i, name := range want {
		if names[i] != name {
			t.Errorf("component %d = %q, want %q (full list %v)", i, names[i], name, names)
		}
	}
}

func TestAttachTextsStampsIDsFromThePool(t *testing.T) {
	// Reads one entry straight out of the pool rather than out of a projection
	// Attach already stamped, so a broken lookup cannot supply its own fixture.
	var key string
	var want ComponentTexts

	for candidate, entry := range textArtifact.Components {
		if entry.Notice != "" {
			key, want = candidate, entry

			break
		}

		if key == "" {
			key, want = candidate, entry
		}
	}

	if key == "" {
		t.Fatal("the embedded text pool maps no components; run 'make sbom'")
	}

	ecosystem, rest, _ := strings.Cut(key, ":")
	name, version, _ := strings.Cut(rest, "@")

	components := []Component{{Name: name, Version: version, Ecosystem: ecosystem}}
	attachTexts(components)

	if components[0].TextID != want.License {
		t.Errorf("%s: textId = %q, want %q", key, components[0].TextID, want.License)
	}

	if components[0].NoticeID != want.Notice {
		t.Errorf("%s: noticeId = %q, want %q", key, components[0].NoticeID, want.Notice)
	}
}
