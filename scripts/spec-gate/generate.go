package main

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
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
// tests do not notice. Not a gate: an equivalent mutant survives honestly, so
// the output is a list to triage, not a verdict.
func runGenerated(root, pkg string) error {
	dir := filepath.Join(root, filepath.Clean(strings.TrimPrefix(pkg, "./")))

	files, err := packageSources(dir)
	if err != nil {
		return err
	}

	type planned struct {
		src []byte
		m   genMutant
	}

	var plan []planned

	for _, path := range files {
		src, err := os.ReadFile(path) // #nosec G304 -- a path this command was given
		if err != nil {
			return err
		}

		mutants, err := generate(path, src)
		if err != nil {
			return err
		}

		for _, m := range mutants {
			plan = append(plan, planned{src, m})
		}
	}

	fmt.Fprintf(os.Stderr, "spec: %d operator mutants across %d files in %s\n",
		len(plan), len(files), pkg)

	var (
		killed, unviable int
		survivors        []genMutant
		errs             []error
		mu               sync.Mutex
		wg               sync.WaitGroup
	)

	// The same slots as runMutants: each swap has its own overlay.
	slots := make(chan struct{}, runtime.NumCPU())

	for _, p := range plan {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			outcome, err := tryGenerated(root, p.m.File, applyGenerated(p.src, p.m), pkg)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err != nil:
				errs = append(errs, err)
			case outcome == unviableMutant:
				unviable++
			case outcome == survivedMutant:
				survivors = append(survivors, p.m)
			default:
				killed++
			}
		})
	}

	wg.Wait()

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	slices.SortFunc(survivors, func(a, b genMutant) int {
		return cmp.Or(strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
	})

	for _, m := range survivors {
		fmt.Fprintf(os.Stderr, "  survived: %s:%d: %s -> %s\n", m.File, m.Line, m.From, m.To)
	}

	survived := len(survivors)

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
	out, err := runOverlaid(root, path, string(mutated), pkg)

	_, failed := errors.AsType[*exec.ExitError](err)

	switch {
	case bytes.Contains(out, []byte(buildFailed)):
		return unviableMutant, nil
	case failed:
		return killedMutant, nil
	case err != nil:
		return killedMutant, err
	default:
		return survivedMutant, nil
	}
}
