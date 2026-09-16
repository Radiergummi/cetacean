package main

import (
	"path/filepath"
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

func TestScanFileRejectsANonLiteralClaim(t *testing.T) {
	p := writeTemp(t, "b_test.go", `package x

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

func TestTwo(t *testing.T) {
	id := "mcp/sep-2575/a"
	spec.Satisfies(t, id)
}
`)

	_, errs := ScanFile(p)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one about the non-literal argument", errs)
	}
}

// The gate matches on the selector name, so an alias would break it silently
// in both directions.
func TestScanFileRejectsAnAliasedImport(t *testing.T) {
	p := writeTemp(t, "c_test.go", `package x

import (
	"testing"

	sp "github.com/radiergummi/cetacean/internal/spec"
)

func TestThree(t *testing.T) {
	sp.Satisfies(t, "mcp/sep-2575/a")
}
`)

	_, errs := ScanFile(p)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one about the import alias", errs)
	}
}

func TestScanFileRejectsACallWithNoArguments(t *testing.T) {
	p := writeTemp(t, "d_test.go", `package x

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

func TestFour(t *testing.T) {
	spec.Satisfies()
}
`)

	_, errs := ScanFile(p)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one about the missing arguments", errs)
	}
}

// A claim registers its cleanup when it runs, and cleanups run
// last-registered-first — so a helper called first registers one that fires
// after the claim's own and can no longer withhold it.
func TestScanFileRejectsAClaimThatIsNotFirst(t *testing.T) {
	const preamble = `package x

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

`

	cases := []struct {
		name    string
		body    string
		refused bool
	}{{
		name: "after a helper that registers a cleanup",
		body: `func TestLate(t *testing.T) {
	s := newTestServer(t)
	spec.Satisfies(t, "mcp/sep-2575/a")
	_ = s
}`,
		refused: true,
	}, {
		name: "first, after t.Helper",
		body: `func TestEarly(t *testing.T) {
	t.Helper()
	spec.Satisfies(t, "mcp/sep-2575/a")
}`,
	}, {
		name: "first, after a declaration",
		body: `func TestAfterConst(t *testing.T) {
	const id = "x"
	spec.Satisfies(t, "mcp/sep-2575/a")
	_ = id
}`,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := ScanFile(writeTemp(t, "o_test.go", preamble+tc.body+"\n"))

			switch {
			case tc.refused && len(errs) != 1:
				t.Fatalf("errors = %v, want one about the call order", errs)
			case tc.refused && !strings.Contains(errs[0].Error(), "must come first"):
				t.Errorf("error does not name the rule: %v", errs[0])
			case !tc.refused && len(errs) != 0:
				t.Fatalf("errors = %v, want none", errs)
			}
		})
	}
}

// A file the ordinary lane compiles has no excuse for a claim that did not
// run, whatever build constraint it carries.
func TestAGOOSConstraintDoesNotMarkAClaimTagged(t *testing.T) {
	const body = `//go:build !windows

package x

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

func TestFour(t *testing.T) {
	spec.Satisfies(t, "mcp/sep-2575/a")
}
`

	claims, errs := ScanFile(writeTemp(t, "d_test.go", body))
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}

	if len(claims) != 1 || claims[0].Tagged {
		t.Fatalf("claim = %+v, want one that is not tagged", claims)
	}
}
