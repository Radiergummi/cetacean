package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	cyclonedx "github.com/CycloneDX/cyclonedx-go"
)

// One package resolved against two sets of peers reaches the closure twice and
// renders to one bom-ref, which the document may not carry twice.
func TestRunEmitsOneComponentPerNameAndVersion(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, lockPath), `lockfileVersion: '9.0'
importers:
  frontend:
    dependencies:
      a:
        specifier: ^1.0.0
        version: 1.0.0(p@1.0.0)
      b:
        specifier: ^1.0.0
        version: 1.0.0(p@2.0.0)
packages:
  a@1.0.0:
    resolution:
      integrity: sha512-3q2+7w==
  b@1.0.0:
    resolution:
      integrity: sha512-3q2+7w==
  shared@1.0.0:
    resolution:
      integrity: sha512-3q2+7w==
snapshots:
  a@1.0.0(p@1.0.0):
    dependencies:
      shared: 1.0.0(p@1.0.0)
  b@1.0.0(p@2.0.0):
    dependencies:
      shared: 1.0.0(p@2.0.0)
  shared@1.0.0(p@1.0.0): {}
  shared@1.0.0(p@2.0.0): {}
`)

	for entry, name := range map[string]string{
		"a@1.0.0_p@1.0.0":      "a",
		"b@1.0.0_p@2.0.0":      "b",
		"shared@1.0.0_p@1.0.0": "shared",
		"shared@1.0.0_p@2.0.0": "shared",
	} {
		dir := filepath.Join(root, storeRoot, entry, "node_modules", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}

		write(t, filepath.Join(dir, "package.json"), `{"license":"MIT"}`)
	}

	out := filepath.Join(t.TempDir(), "npm.cdx.json")

	t.Chdir(root)

	if err := run("frontend@0.0.0", out); err != nil {
		t.Fatalf("run: %v", err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	var document cyclonedx.BOM
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}

	refs := map[string]int{}
	for _, component := range *document.Components {
		refs[component.BOMRef]++
	}

	if refs["frontend@0.0.0|shared@1.0.0"] != 1 {
		t.Errorf("shared rendered %d times; want 1", refs["frontend@0.0.0|shared@1.0.0"])
	}

	if len(*document.Components) != 3 {
		t.Errorf("got %d components; want 3: %v", len(*document.Components), refs)
	}
}
