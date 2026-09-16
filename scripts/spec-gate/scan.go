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
	"strconv"
)

// specImport is the only import path a claim may come from, and it may not be
// aliased: the scan has no type information and matches the selector name.
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

func Scan(root string) ([]Claim, []error) {
	files, err := TestFiles(root)
	if err != nil {
		return nil, []error{err}
	}

	var (
		claims []Claim
		errs   []error
	)

	for _, f := range files {
		c, e := ScanFile(f)
		claims = append(claims, c...)
		errs = append(errs, e...)
	}

	return claims, errs
}

// ScanFile parses one test file. go/parser applies no build constraints, which
// is what lets this reach test/e2e at all.
func ScanFile(path string) ([]Claim, []error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, []error{fmt.Errorf("%s: %w", path, err)}
	}

	var errs []error

	imported, alias := importState(file)
	if alias != "" {
		errs = append(errs, fmt.Errorf(
			"%s: internal/spec is imported as %q; claims are matched by selector name, so it must not be aliased",
			path,
			alias,
		))
	}

	if !imported {
		return nil, errs
	}

	tagged := needsABuildTag(file)

	var claims []Claim

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
			if !ok || sel.Sel.Name != "Satisfies" {
				return true
			}

			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "spec" {
				return true
			}

			if err := claimOrder(path, fset, fn, call.Pos()); err != nil {
				errs = append(errs, err)
			}

			if len(call.Args) == 0 {
				errs = append(errs, fmt.Errorf(
					"%s:%d: %s calls Satisfies with no arguments",
					path,
					fset.Position(call.Pos()).Line,
					fn.Name.Name,
				))

				return true
			}

			for _, arg := range call.Args[1:] {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					errs = append(errs, fmt.Errorf(
						"%s:%d: %s passes a non-literal requirement id; the gate cannot see through it",
						path,
						fset.Position(arg.Pos()).Line,
						fn.Name.Name,
					))

					continue
				}

				id, err := strconv.Unquote(lit.Value)
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", path, err))

					continue
				}

				claims = append(claims, Claim{
					ID:     id,
					File:   path,
					Func:   fn.Name.Name,
					Line:   fset.Position(lit.Pos()).Line,
					Tagged: tagged,
				})
			}

			return true
		})
	}

	return claims, errs
}

// claimOrder refuses a claim that is not the first thing its function does.
// Cleanups run last-registered-first, so a helper called earlier registers one
// that runs AFTER the claim's own and cannot withhold it for a test that helper
// then fails. A claim nested inside a closure is not checked.
func claimOrder(path string, fset *token.FileSet, fn *ast.FuncDecl, pos token.Pos) error {
	if fn.Body == nil {
		return nil
	}

	for _, stmt := range fn.Body.List {
		if pos >= stmt.Pos() && pos <= stmt.End() {
			return nil
		}

		var earlier *ast.CallExpr

		ast.Inspect(stmt, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || earlier != nil {
				return true
			}

			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Helper" {
				return true
			}

			earlier = call

			return false
		})

		if earlier != nil {
			return fmt.Errorf(
				"%s:%d: %s calls Satisfies after other work; it must come first, "+
					"or a cleanup registered earlier runs after the claim and cannot withhold it",
				path, fset.Position(pos).Line, fn.Name.Name,
			)
		}
	}

	return nil
}

// importState reports whether internal/spec is imported and under what alias.
func importState(file *ast.File) (bool, string) {
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != specImport {
			continue
		}

		if imp.Name != nil && imp.Name.Name != "spec" {
			return true, imp.Name.Name
		}

		return true, ""
	}

	return false, ""
}

// needsABuildTag reports whether a default `go test ./...` skips this file —
// that is, whether its constraint is false with no tag set. A file excluded
// only by GOOS, like //go:build !windows, still runs in the ordinary lane and
// has no excuse for a claim that did not.
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

			if !expr.Eval(func(string) bool { return false }) {
				return true
			}
		}
	}

	return false
}
