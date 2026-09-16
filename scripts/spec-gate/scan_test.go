package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return p
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
