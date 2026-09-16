package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormaliseCitation(t *testing.T) {
	for in, want := range map[string]string{
		"RFC 7636": "RFC7636",
		"RFC7636":  "RFC7636",
		"SEP-2575": "SEP-2575",
		"SEP 2575": "SEP-2575",
		"SEP2575":  "SEP-2575",
	} {
		if got := normaliseCitation(in); got != want {
			t.Errorf("normaliseCitation(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestACitedButUnregisteredSpecificationFailsTheSweep(t *testing.T) {
	cited := map[string][]string{
		"RFC7636": {"internal/oauth/server.go"},
		"RFC7009": {"internal/oauth/server.go"},
	}

	registered := map[string]bool{"RFC7636": true}
	dismissed := map[string]string{}

	errs := sweepErrors(cited, registered, dismissed)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "RFC7009") {
		t.Fatalf("errors = %v, want one naming RFC7009", errs)
	}
}

func TestADismissalNeedsAReason(t *testing.T) {
	cited := map[string][]string{"RFC3339": {"internal/api/x.go"}}

	errs := sweepErrors(cited, map[string]bool{}, map[string]string{"RFC3339": "  "})
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one about the empty reason", errs)
	}
}

func TestAStaleDismissalFailsTheSweep(t *testing.T) {
	errs := sweepErrors(
		map[string][]string{},
		map[string]bool{},
		map[string]string{"RFC9999": "nothing cites this any more"},
	)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "RFC9999") {
		t.Fatalf("errors = %v, want one about the stale dismissal", errs)
	}
}

// TestCitationsExcludesTheRegistryTreeFromItsOwnSweep guards against a token
// self-citing: unregistered.yaml writes "RFC9999:" as a map key, and a
// registered document names its own RFC in "source:"/"url:". Neither is a
// real citation, or a stale dismissal could never be caught.
func TestCitationsExcludesTheRegistryTreeFromItsOwnSweep(t *testing.T) {
	root := t.TempDir()

	writeFixtureFile(t, root, "internal/spec/registry/unregistered.yaml",
		"RFC9999: a token only this file names\n")
	writeFixtureFile(t, root, "internal/real.go",
		"// package cites.go names nothing here.\npackage internal\n")
	gitInitFixture(t, root)

	cited, err := Citations(root)
	if err != nil {
		t.Fatal(err)
	}

	if files, ok := cited["RFC9999"]; ok {
		t.Fatalf("RFC9999 reported as cited (%v); the registry tree self-cites", files)
	}
}

func writeFixtureFile(t *testing.T, root, rel, body string) {
	t.Helper()

	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gitInitFixture(t *testing.T, root string) {
	t.Helper()

	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		// #nosec G204 -- args are fixed literals plus t.TempDir(), never
		// caller input.
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
