package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"strconv"
	"strings"
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
	cmd := exec.CommandContext(
		context.Background(),
		"git",
		"-C",
		root,
		"ls-files",
		"-z",
		"*_test.go",
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	var files []string

	for p := range bytes.SplitSeq(out, []byte{0}) {
		if len(p) > 0 {
			files = append(files, root+"/"+string(p))
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

	tagged := hasBuildConstraint(file)

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

func hasBuildConstraint(file *ast.File) bool {
	for _, group := range file.Comments {
		for _, c := range group.List {
			if strings.HasPrefix(c.Text, "//go:build") {
				return true
			}
		}
	}

	return false
}
