package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const boundarySrc = `package p

func atLeast(n int) bool {
	if n < 43 {
		return false
	}

	return true
}
`

func TestGeneratingSwapsAComparisonBoundary(t *testing.T) {
	got, err := generate("p.go", []byte(boundarySrc))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Fatalf("mutants = %+v, want exactly one", got)
	}

	m := got[0]
	if m.From != "<" || m.To != "<=" {
		t.Errorf("swap = %q -> %q, want < -> <=", m.From, m.To)
	}

	if m.Line != 4 {
		t.Errorf("line = %d, want 4", m.Line)
	}
}

func TestApplyingAGeneratedMutantEditsOnlyTheOperator(t *testing.T) {
	got, err := generate("p.go", []byte(boundarySrc))
	if err != nil {
		t.Fatal(err)
	}

	want := `package p

func atLeast(n int) bool {
	if n <= 43 {
		return false
	}

	return true
}
`

	if out := string(applyGenerated([]byte(boundarySrc), got[0])); out != want {
		t.Errorf("mutated source:\n%s\nwant:\n%s", out, want)
	}
}

func TestGeneratingCoversEverySwappableOperatorInAnExpression(t *testing.T) {
	got, err := generate("p.go", []byte("package p\n\nfunc f() bool { return 1 == 2 || 3 != 4 }\n"))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 3 {
		t.Fatalf("mutants = %d, want 3 (==, ||, !=)", len(got))
	}
}

// A test file's own operators are not the subject: mutating them would report
// on the suite rather than on the code the suite covers.
func TestPackageSourcesLeavesTestFilesAlone(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"server.go", "server_test.go", "keys.go", "notes.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package p\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := packageSources(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{filepath.Join(dir, "keys.go"), filepath.Join(dir, "server.go")}
	if !slices.Equal(got, want) {
		t.Errorf("sources = %v, want %v", got, want)
	}
}
