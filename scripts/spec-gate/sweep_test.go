package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

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

// TestCitationREMatchesAnUnnumberedSpecification covers the documents that
// have no RFC number to be named by. OAuth 2.1 is one, and the tree names it
// in prose eighteen times.
func TestCitationREMatchesAnUnnumberedSpecification(t *testing.T) {
	// The registry files the document as oauth-2-1.yaml, and the tree names it
	// in prose. Both sides canonicalise, so both have to land on one token.
	if got := spec.Token("oauth-2-1"); got != "OAUTH21" {
		t.Errorf("the document name tokenises to %q, want OAUTH21", got)
	}

	cases := map[string]string{
		"OAuth 2.1 authorization server": "OAUTH21",
		"per OAuth 2.1 Section 2.3.1":    "OAUTH21",
		"RFC 7636":                       "RFC7636",
	}

	for input, want := range cases {
		match := citationRE.FindString(input)
		if match == "" {
			t.Errorf("%q matched nothing, want %s", input, want)

			continue
		}
		if got := spec.Token(match); got != want {
			t.Errorf("%q: token = %q, want %q", input, got, want)
		}
	}

	// OAuth 2.0 is the predecessor, named all over the RFCs this tree cites,
	// and is not this document.
	if match := citationRE.FindString("the OAuth 2.0 framework"); match != "" {
		t.Errorf("OAuth 2.0 matched %q", match)
	}
}
