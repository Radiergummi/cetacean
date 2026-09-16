package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	cyclonedx "github.com/CycloneDX/cyclonedx-go"
)

// npm's integrity is base64 and CycloneDX wants hex, so a digest that survives
// the trip is the only evidence the conversion is right.
func TestIntegrityHashesDecodesToHex(t *testing.T) {
	got := integrityHashes("sha512-3q2+7w==")
	if got == nil {
		t.Fatal("sha512 integrity was rejected")
	}

	if (*got)[0].Algorithm != cyclonedx.HashAlgoSHA512 || (*got)[0].Value != "deadbeef" {
		t.Errorf("got %+v; want SHA-512/deadbeef", (*got)[0])
	}

	for _, bad := range []string{"", "deadbeef", "md5-3q2+7w==", "sha512-not base64!"} {
		if integrityHashes(bad) != nil {
			t.Errorf("integrityHashes(%q) should be nil", bad)
		}
	}
}

func TestPurlEscapesTheScope(t *testing.T) {
	if got := purl("@base-ui", "react", "1.8.0"); got != "pkg:npm/%40base-ui/react@1.8.0" {
		t.Errorf("scoped purl = %q", got)
	}

	if got := purl("", "react", "19.3.0"); got != "pkg:npm/react@19.3.0" {
		t.Errorf("unscoped purl = %q", got)
	}
}

func TestLicensesAcceptsEveryShapeInTheWild(t *testing.T) {
	id := licenses(json.RawMessage(`"MIT"`))
	if id == nil || (*id)[0].License == nil || (*id)[0].License.ID != "MIT" {
		t.Errorf("identifier: got %+v", id)
	}

	expression := licenses(json.RawMessage(`"(MIT OR Apache-2.0)"`))
	if expression == nil || (*expression)[0].Expression != "(MIT OR Apache-2.0)" {
		t.Errorf("expression: got %+v", expression)
	}

	object := licenses(json.RawMessage(`{"type":"BSD-3-Clause","url":"x"}`))
	if object == nil || (*object)[0].License == nil || (*object)[0].License.ID != "BSD-3-Clause" {
		t.Errorf("object: got %+v", object)
	}

	for _, empty := range []string{``, `""`, `null`, `[]`} {
		if licenses(json.RawMessage(empty)) != nil {
			t.Errorf("licenses(%q) should be nil", empty)
		}
	}
}

func TestPersonNameDropsContactDetails(t *testing.T) {
	cases := map[string]string{
		`"MUI Team"`:                            "MUI Team",
		`"Sindre <s@example.com> (sindre.dev)"`: "Sindre",
		`"Ada (ada.dev)"`:                       "Ada",
		`{"name":"Grace","email":"g@e.com"}`:    "Grace",
		`{}`:                                    "",
		``:                                      "",
	}

	for raw, want := range cases {
		if got := personName(json.RawMessage(raw)); got != want {
			t.Errorf("personName(%s) = %q; want %q", raw, got, want)
		}
	}
}

// A store entry is named for the package with `/` replaced by `+`, and carries
// a suffix naming the peers it was resolved against.
func TestStoreDirFindsPeerSuffixedEntries(t *testing.T) {
	store := t.TempDir()
	dir := filepath.Join(
		store,
		"@base-ui+react@1.8.0_react@19.3.0",
		"node_modules",
		"@base-ui",
		"react",
	)

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	opened, err := openStore(store)
	if err != nil {
		t.Fatal(err)
	}

	got, err := opened.dir("@base-ui/react", "1.8.0")
	if err != nil {
		t.Fatalf("storeDir: %v", err)
	}

	if got != dir {
		t.Errorf("storeDir = %q; want %q", got, dir)
	}

	// A longer version must not be matched by a shorter one's prefix.
	if _, err := opened.dir("@base-ui/react", "1.8"); err == nil {
		t.Error("1.8 should not match the entry for 1.8.0")
	}
}

func TestStoreDirReportsUninstalledPackages(t *testing.T) {
	if _, err := mustOpen(t, t.TempDir()).dir("only-on-windows", "1.0.0"); err == nil {
		t.Fatal("expected errNotInstalled")
	}
}

// The lockfile is an input, so a name that would walk out of the store has to
// be refused rather than resolved.
func TestStoreDirRefusesTraversal(t *testing.T) {
	store := t.TempDir()

	for _, bad := range []struct{ name, version string }{
		{"../../etc", "1.0.0"},
		{"@scope/../../etc", "1.0.0"},
		{"a/b/c", "1.0.0"},
		{"pkg", "../../1.0.0"},
		{"pkg", "1.0.0/../.."},
		{"/absolute", "1.0.0"},
	} {
		if _, err := mustOpen(t, store).dir(bad.name, bad.version); err == nil {
			t.Errorf("storeDir(%q, %q) was accepted", bad.name, bad.version)
		}
	}

	// Ordinary names must still resolve.
	for _, good := range []string{"react", "@base-ui/react", "lodash.merge", "vue-demi"} {
		if _, err := mustOpen(t, store).dir(good, "1.0.0"); errors.Is(err, nil) {
			t.Errorf("%q should reach the not-installed path, not be refused", good)
		}
	}
}

func mustOpen(t *testing.T, root string) *store {
	t.Helper()

	opened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}

	return opened
}
