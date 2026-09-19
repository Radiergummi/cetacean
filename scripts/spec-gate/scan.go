package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// specImport is the only import path a claim may come from. It may not be
// aliased and its ids must be literals, because this scan has no type
// information — scripts/spec-vet is what holds callers to both.
const specImport = "github.com/radiergummi/cetacean/internal/spec"

type Claim struct {
	ID   string
	File string
	Func string
	Line int

	// Tagged marks a claim behind a build constraint, which CI compiles but
	// never runs. The report needs it to say "not run" rather than "uncovered".
	Tagged bool
}

// TestFiles lists the repository's tracked test files. A filesystem walk would
// also find .worktrees/, whose copies of this tree would mask uncovered
// requirements; go list would omit test/e2e entirely, because its build
// constraint excludes every file in the package.
func TestFiles(root string) ([]string, error) {
	rel, err := gitLsFiles(root, "*_test.go")
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(rel))

	for _, p := range rel {
		if slices.Contains(strings.Split(p, "/"), "testdata") {
			continue
		}

		files = append(files, root+"/"+p)
	}

	return files, nil
}

// gitLsFiles lists tracked files matching args, relative to root. Tracked, not
// walked: a walk also finds .worktrees/ copies of this tree, whose claims would
// mask uncovered requirements here.
func gitLsFiles(root string, args ...string) ([]string, error) {
	argv := append([]string{"-C", root, "ls-files", "-z"}, args...)

	out, err := exec.CommandContext(context.Background(), "git", argv...).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	var files []string

	for p := range bytes.SplitSeq(out, []byte{0}) {
		if len(p) > 0 {
			files = append(files, string(p))
		}
	}

	return files, nil
}

// Scan returns what the tree claims and what it observes, separately. An
// observation establishes nothing on its own, and a consumer handed both in
// one slice has to remember to filter — which is a rule no type enforces, in
// three commands that do not run together.
func Scan(root string) (claims, observations []Claim, errs []error) {
	files, err := TestFiles(root)
	if err != nil {
		return nil, nil, []error{err}
	}

	for _, f := range files {
		c, o, e := ScanFile(f)
		claims = append(claims, c...)
		observations = append(observations, o...)
		errs = append(errs, e...)
	}

	return claims, observations, errs
}

// ScanFile extracts the claims one test file makes. go/parser applies no build
// constraints, which is what lets this reach test/e2e at all; the price is no
// type information, so how a claim may be written is scripts/spec-vet's to
// enforce.
func ScanFile(path string) (claims, observations []Claim, errs []error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, []error{fmt.Errorf("%s: %w", path, err)}
	}

	if !importsSpec(file) {
		return nil, nil, nil
	}

	tagged := needsABuildTag(file)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			observation := sel.Sel.Name == "Observed"
			if sel.Sel.Name != "Satisfies" && !observation {
				return true
			}

			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "spec" {
				return true
			}

			// go/parser applies no type checking, so a call that names no
			// arguments at all reaches here even though it cannot compile.
			if len(call.Args) == 0 {
				return true
			}

			// Satisfies takes every argument after t as an id; Observed takes
			// one, and a format string after it.
			ids := call.Args[1:]
			if observation {
				ids = ids[:min(1, len(ids))]
			}

			for _, arg := range ids {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					errs = append(errs, unreadable(path, fset, arg.Pos()))

					continue
				}

				id, err := strconv.Unquote(lit.Value)
				if err != nil {
					errs = append(errs, unreadable(path, fset, arg.Pos()))

					continue
				}

				found := Claim{
					ID:     id,
					File:   path,
					Func:   fn.Name.Name,
					Line:   fset.Position(lit.Pos()).Line,
					Tagged: tagged,
				}

				if observation {
					observations = append(observations, found)
				} else {
					claims = append(claims, found)
				}
			}

			return true
		})
	}

	return claims, observations, errs
}

// unreadable reports a claim this scan could not extract an id from, which
// would otherwise leave the inventory short without anything saying so — a
// requirement with another claimant stays green. Why the form is refused is
// scripts/spec-vet's to explain.
func unreadable(path string, fset *token.FileSet, pos token.Pos) error {
	return fmt.Errorf("%s:%d: claim id is not a string literal; this claim is not counted",
		path, fset.Position(pos).Line)
}

// importsSpec reports whether this file can contain a claim at all.
func importsSpec(file *ast.File) bool {
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == specImport {
			return true
		}
	}

	return false
}

// needsABuildTag reports whether a default `go test ./...` skips this file —
// that is, whether its constraint is false with nothing but this platform's
// own tags set. A file excluded only by GOOS still runs in the ordinary lane
// and has no excuse for a claim that did not.
func needsABuildTag(file *ast.File) bool {
	for _, group := range file.Comments {
		for _, c := range group.List {
			if !constraint.IsGoBuild(c.Text) {
				continue
			}

			expr, err := constraint.Parse(c.Text)
			if err != nil {
				continue
			}

			if !expr.Eval(platformTag) {
				return true
			}
		}
	}

	return false
}

// platformTag reports whether the toolchain sets this tag for an ordinary
// build here. Only the implicit ones: a tag the lane would have to be asked
// for is what this is trying to find.
func platformTag(tag string) bool {
	switch tag {
	case runtime.GOOS, runtime.GOARCH:
		return true
	case "unix":
		return runtime.GOOS != "windows" && runtime.GOOS != "js" &&
			runtime.GOOS != "plan9" && runtime.GOOS != "wasip1"
	default:
		return false
	}
}
