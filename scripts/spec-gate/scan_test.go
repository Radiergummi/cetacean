package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()

	root := t.TempDir()
	writeFixtureFile(t, root, name, body)

	return filepath.Join(root, name)
}

func TestScanFileFindsLiteralClaims(t *testing.T) {
	p := writeTemp(t, "a_test.go", `//go:build e2e

package e2e_test

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

func TestOne(t *testing.T) {
	spec.Satisfies(t, "mcp/sep-2575/a", "mcp/sep-2575/b")
}
`)

	claims, errs := ScanFile(p)
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}

	if len(claims) != 2 {
		t.Fatalf("claims = %+v, want 2", claims)
	}

	if claims[0].Func != "TestOne" || !claims[0].Tagged {
		t.Errorf("claim = %+v, want TestOne behind a build tag", claims[0])
	}
}

// How a claim may be written is scripts/spec-vet's to enforce; what this scan
// must not do is invent one. Each case below is invisible to it, which leaves
// the requirement looking unclaimed — the direction that fails closed.
func TestScanFileReadsNoClaimItCannotSee(t *testing.T) {
	const preamble = `package x

import (
	"testing"

	%s"github.com/radiergummi/cetacean/internal/spec"
)

`

	cases := []struct {
		name  string
		alias string
		body  string
	}{{
		name: "a non-literal id",
		body: `func TestTwo(t *testing.T) {
	id := "mcp/sep-2575/a"
	spec.Satisfies(t, id)
}`,
	}, {
		name:  "an aliased import",
		alias: "sp ",
		body: `func TestThree(t *testing.T) {
	sp.Satisfies(t, "mcp/sep-2575/a")
}`,
	}, {
		name: "a call naming no requirement",
		body: `func TestFour(t *testing.T) {
	spec.Satisfies(t)
}`,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf(preamble, tc.alias) + tc.body + "\n"

			claims, errs := ScanFile(writeTemp(t, "o_test.go", src))
			if len(errs) != 0 {
				t.Fatalf("errors = %v, want none — spec-vet reports these", errs)
			}

			if len(claims) != 0 {
				t.Fatalf("claims = %+v, want none", claims)
			}
		})
	}
}

// Fixtures under testdata are not code of ours, and spec-vet keeps claims
// there that name requirements the registry does not have.
func TestTestFilesSkipsTestdata(t *testing.T) {
	files, err := TestFiles(".")
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if slices.Contains(strings.Split(f, "/"), "testdata") {
			t.Errorf("%s is under testdata", f)
		}
	}
}

// A file the ordinary lane compiles has no excuse for a claim that did not
// run, whether its constraint names this platform or merely spares it.
func TestAGOOSConstraintDoesNotMarkAClaimTagged(t *testing.T) {
	const body = `//go:build %s

package x

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

func TestFour(t *testing.T) {
	spec.Satisfies(t, "mcp/sep-2575/a")
}
`

	for _, constraint := range []string{"!windows", runtime.GOOS, runtime.GOARCH} {
		t.Run(constraint, func(t *testing.T) {
			source := writeTemp(t, "d_test.go", fmt.Sprintf(body, constraint))

			claims, errs := ScanFile(source)
			if len(errs) != 0 {
				t.Fatalf("errors = %v", errs)
			}

			if len(claims) != 1 || claims[0].Tagged {
				t.Fatalf("claim = %+v, want one that is not tagged", claims)
			}
		})
	}
}
