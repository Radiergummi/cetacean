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

func TestAttachPopulatesTextAndNoticeIDs(t *testing.T) {
	// Unit test of Attach: verify it correctly looks up and stamps components
	// with TextID and NoticeID from the pool. Fails loudly if Attach is deleted
	// or broken, and stays correct across dependency bumps (uses real pool data).

	// Get the current projection to find a component that has ids populated.
	doc, err := Project(Raw())
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	// Find a component that Attach populated in the projection.
	var templateComponent Component
	for _, component := range doc.Components {
		if component.Ecosystem != "other" && component.TextID != "" {
			templateComponent = component
			break
		}
	}

	if templateComponent.Name == "" {
		// If no component was populated, Attach either isn't running or the pool
		// is empty. Fail with a diagnostic message.
		t.Fatal(
			"no component with textId in the projection; " +
				"Attach() is not populating components from the pool",
		)
	}

	// Create a new component with the same identity but blank ids, simulating
	// an unattached component, then verify Attach repopulates it.
	testComponent := Component{
		Name:      templateComponent.Name,
		Version:   templateComponent.Version,
		Ecosystem: templateComponent.Ecosystem,
	}

	components := []Component{testComponent}
	Attach(components)

	if components[0].TextID == "" {
		t.Errorf(
			"%s: Attach did not populate textId",
			templateComponent.Name,
		)
	}

	if components[0].TextID != templateComponent.TextID {
		t.Errorf(
			"%s: textId mismatch; Attach got %q, expected %q",
			templateComponent.Name, components[0].TextID, templateComponent.TextID,
		)
	}

	// NoticeID is optional; only verify if the template had one.
	if templateComponent.NoticeID != "" {
		if components[0].NoticeID != templateComponent.NoticeID {
			t.Errorf(
				"%s: noticeId mismatch; Attach got %q, expected %q",
				templateComponent.Name, components[0].NoticeID, templateComponent.NoticeID,
			)
		}
	}
}
