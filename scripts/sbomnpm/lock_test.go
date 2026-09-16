package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSplitKeySeparatesScopeFromVersion(t *testing.T) {
	cases := map[string]struct{ name, version string }{
		"react@19.3.0":                      {"react", "19.3.0"},
		"@base-ui/react@1.8.0":              {"@base-ui/react", "1.8.0"},
		"@ai-sdk/gateway@3.0.13(zod@4.6.5)": {"@ai-sdk/gateway", "3.0.13"},
		"react-dom@19.3.0(react@19.3.0)":    {"react-dom", "19.3.0"},
		"@scope/pkg@1.0.0(a@1)(b@2(c@3))":   {"@scope/pkg", "1.0.0"},
		"typescript@7.0.2-beta.1":           {"typescript", "7.0.2-beta.1"},
	}

	for key, want := range cases {
		name, version := splitKey(key)
		if name != want.name || version != want.version {
			t.Errorf(
				"splitKey(%q) = %q, %q; want %q, %q",
				key,
				name,
				version,
				want.name,
				want.version,
			)
		}
	}
}

// The package manager's own document is written first and names no workspace
// package, so picking the first would describe pnpm rather than the project.
func TestLoadLockfileSkipsThePackageManagerDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pnpm-lock.yaml")
	write(t, path, `---
lockfileVersion: '9.0'
importers:
  .:
    packageManagerDependencies:
      pnpm:
        version: 12.4.2
packages:
  '@pnpm/exe.linux-x64@12.4.2': {}
---
lockfileVersion: '9.0'
importers:
  frontend:
    dependencies:
      react:
        specifier: ^19.0.0
        version: 19.3.0
packages:
  react@19.3.0:
    resolution:
      integrity: sha512-AAAA
snapshots:
  react@19.3.0: {}
`)

	lock, err := loadLockfile(path, "frontend")
	if err != nil {
		t.Fatalf("loadLockfile: %v", err)
	}

	if _, ok := lock.Packages["react@19.3.0"]; !ok {
		t.Fatalf("read the wrong document: packages = %v", lock.Packages)
	}
}

func TestLoadLockfileReportsAnAbsentImporter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pnpm-lock.yaml")
	write(t, path, "lockfileVersion: '9.0'\nimporters:\n  frontend: {}\n")

	if _, err := loadLockfile(path, "website"); err == nil {
		t.Fatal("expected an error naming the missing importer")
	}
}

// A lockfile that will not parse is the likelier failure of the two, and
// reporting it as an absent importer sends the reader to the wrong question.
func TestLoadLockfileReportsAMalformedDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pnpm-lock.yaml")
	write(t, path, "importers:\n\t- tab indentation is not YAML\n")

	_, err := loadLockfile(path, "frontend")
	if err == nil {
		t.Fatal("expected an error")
	}

	if strings.Contains(err.Error(), "declares importer") {
		t.Errorf("parse failure reported as a missing importer: %v", err)
	}
}

// Development dependencies never appear in the importer entry, so the closure
// cannot reach them; optional ones ship and must be reached.
func TestClosureWalksProductionOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pnpm-lock.yaml")
	write(t, path, `lockfileVersion: '9.0'
importers:
  frontend:
    dependencies:
      root:
        specifier: ^1.0.0
        version: 1.0.0
    devDependencies:
      tooling:
        specifier: ^1.0.0
        version: 1.0.0
packages: {}
snapshots:
  root@1.0.0:
    dependencies:
      transitive: 2.0.0
    optionalDependencies:
      platform: 3.0.0
  transitive@2.0.0:
    dependencies:
      root: 1.0.0
  platform@3.0.0: {}
  tooling@1.0.0: {}
`)

	lock, err := loadLockfile(path, "frontend")
	if err != nil {
		t.Fatalf("loadLockfile: %v", err)
	}

	got := lock.closure("frontend")
	slices.Sort(got)

	want := []string{"platform@3.0.0", "root@1.0.0", "transitive@2.0.0"}
	if !slices.Equal(got, want) {
		t.Errorf("closure = %v; want %v", got, want)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
