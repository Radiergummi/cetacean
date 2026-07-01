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

func TestProjectInvalidJSON(t *testing.T) {
	if _, err := Project([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
