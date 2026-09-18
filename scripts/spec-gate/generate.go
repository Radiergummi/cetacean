package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// genMutant is one operator swap at a byte offset in a source file. It carries
// the offset rather than the surrounding text a registry mutant matches on,
// because an operator repeats too often in a file for a string to locate it.
type genMutant struct {
	File   string
	Line   int
	Offset int
	From   string
	To     string
}

// swaps is the operator table. Each pair is an edit a test that pins the
// behaviour on one side of the operator should refuse — a boundary moved by
// one, an equality inverted, a conjunction loosened.
var swaps = map[token.Token]token.Token{ //nolint:gochecknoglobals // a constant table
	token.LSS:  token.LEQ,
	token.LEQ:  token.LSS,
	token.GTR:  token.GEQ,
	token.GEQ:  token.GTR,
	token.EQL:  token.NEQ,
	token.NEQ:  token.EQL,
	token.LAND: token.LOR,
	token.LOR:  token.LAND,
}

// generate reports every operator swap available in one file's source.
func generate(filename string, src []byte) ([]genMutant, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}

	var out []genMutant

	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}

		to, ok := swaps[expr.Op]
		if !ok {
			return true
		}

		at := fset.Position(expr.OpPos)
		out = append(out, genMutant{
			File:   filename,
			Line:   at.Line,
			Offset: at.Offset,
			From:   expr.Op.String(),
			To:     to.String(),
		})

		return true
	})

	return out, nil
}

// applyGenerated splices the swap in, leaving every other byte alone so the
// overlaid file differs from the original by the operator and nothing else.
func applyGenerated(src []byte, m genMutant) []byte {
	out := make([]byte, 0, len(src)+len(m.To))
	out = append(out, src[:m.Offset]...)
	out = append(out, m.To...)

	return append(out, src[m.Offset+len(m.From):]...)
}

// packageSources lists the files of one package worth mutating: a Go package
// is a single directory, so this does not recurse.
func packageSources(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	var out []string

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		out = append(out, filepath.Join(dir, name))
	}

	slices.Sort(out)

	return out, nil
}

// runGenerated swaps every operator in a package and reports the swaps its own
// tests do not notice.
//
// This is not a gate. An equivalent mutant — one that cannot change behaviour —
// survives honestly, and no threshold tells it apart from a real hole. The
// output is a list to triage: a survivor worth keeping becomes a registry
// mutant, attached to the requirement it breaks.
func runGenerated(root, pkg string) error {
	dir := filepath.Join(root, filepath.Clean(strings.TrimPrefix(pkg, "./")))

	files, err := packageSources(dir)
	if err != nil {
		return err
	}

	sources := map[string][]byte{}
	planned := map[string][]genMutant{}
	total := 0

	for _, path := range files {
		src, err := os.ReadFile(path) // #nosec G304 -- a path this command was given
		if err != nil {
			return err
		}

		mutants, err := generate(path, src)
		if err != nil {
			return err
		}

		sources[path] = src
		planned[path] = mutants
		total += len(mutants)
	}

	fmt.Fprintf(os.Stderr, "spec: %d operator mutants across %d files in %s\n",
		total, len(files), pkg)

	var survived, killed, unviable int

	for _, path := range files {
		src := sources[path]

		for _, m := range planned[path] {
			switch outcome, err := tryGenerated(root, path, applyGenerated(src, m), pkg); {
			case err != nil:
				return err
			case outcome == unviableMutant:
				unviable++
			case outcome == survivedMutant:
				survived++

				fmt.Fprintf(os.Stderr, "  survived: %s:%d: %s -> %s\n",
					m.File, m.Line, m.From, m.To)
			default:
				killed++
			}
		}
	}

	fmt.Fprintf(os.Stderr,
		"spec: %d generated mutants in %s — %d killed, %d survived, %d did not compile\n",
		killed+survived+unviable, pkg, killed, survived, unviable)

	return nil
}

type outcome int

const (
	killedMutant outcome = iota
	survivedMutant
	unviableMutant
)

// tryGenerated overlays one swap and runs the package's whole suite against it.
// Unlike a registry mutant there is no claimant to narrow the run to, so every
// test in the package is the jury.
func tryGenerated(root, path string, mutated []byte, pkg string) (outcome, error) {
	dir, err := os.MkdirTemp("", "spec-generated-")
	if err != nil {
		return killedMutant, err
	}

	defer os.RemoveAll(dir) //nolint:errcheck // scratch directory

	overlay, err := overlayFor(dir, path, string(mutated))
	if err != nil {
		return killedMutant, err
	}

	// #nosec G204 -- the package is this command's own argument and the overlay
	// is a path in a scratch directory.
	cmd := exec.CommandContext(
		context.Background(), "go", "test", "-overlay="+overlay, "-count=1", pkg,
	)
	cmd.Dir = root

	out, err := cmd.CombinedOutput()

	switch {
	case strings.Contains(string(out), "[build failed]"):
		return unviableMutant, nil
	case err != nil:
		return killedMutant, nil
	default:
		return survivedMutant, nil
	}
}
