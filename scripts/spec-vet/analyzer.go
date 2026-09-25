package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/radiergummi/cetacean/internal/spec"
)

// Analyzer enforces how a test claims a requirement. scripts/spec-gate reads
// the same calls with go/parser and no type information, so two of these rules
// exist for its benefit rather than the compiler's.
var Analyzer = &analysis.Analyzer{
	Name: "specclaim",
	Doc:  "check that spec.Satisfies is called the way the requirement gate can read it",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	if !imports(pass.Pkg) {
		return nil, nil
	}

	for _, file := range pass.Files {
		checkImport(pass, file)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			switch name := callee(pass, call); name {
			case "Satisfies", "Observed":
				checkCall(pass, file, call, name)
			}

			return true
		})
	}

	return nil, nil
}

// callee names the internal/spec function this call resolves to, empty for
// anything else — including internal/spec's own readers, which the gate's
// commands call from ordinary code. Resolved rather than matched by spelling,
// which is what the parser-based scan cannot do.
func callee(pass *analysis.Pass, call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}

	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != spec.ImportPath {
		return ""
	}

	return fn.Name()
}

// checkImport holds the no-alias rule, which exists for scripts/spec-gate
// rather than for anything here.
func checkImport(pass *analysis.Pass, file *ast.File) {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != spec.ImportPath {
			continue
		}

		if imp.Name != nil && imp.Name.Name != "spec" {
			pass.Reportf(imp.Pos(),
				"internal/spec is imported as %q; the requirement gate matches the selector "+
					"name, so an alias hides every claim in this file",
				imp.Name.Name)
		}
	}
}

// imports reports whether this package can contain a claim at all. It is also
// false for internal/spec itself, whose own tests call Satisfies to exercise
// it rather than to claim through it.
func imports(pkg *types.Package) bool {
	if pkg.Path() == spec.ImportPath {
		return false
	}

	for _, dep := range pkg.Imports() {
		if dep.Path() == spec.ImportPath {
			return true
		}
	}

	return false
}

// checkCall holds both entry points to the form the requirement gate can read.
// They differ in two places only: Observed takes one id and a format string
// after it, and its record is written after the assertion that produced it, so
// the ordering rule Satisfies obeys cannot apply to it.
func checkCall(pass *analysis.Pass, file *ast.File, call *ast.CallExpr, name string) {
	if !strings.HasSuffix(pass.Fset.Position(call.Pos()).Filename, "_test.go") {
		pass.Reportf(call.Pos(),
			"%s is called outside a _test.go file; internal/spec imports testing, "+
				"and nothing shipped may link it", name)
	}

	if !inFuncDecl(file, call.Pos()) {
		pass.Reportf(call.Pos(),
			"%s is called outside a function declaration; the requirement gate attributes "+
				"a claim to the test function it sits in, and this one has none", name)
	}

	if len(call.Args) < 2 {
		pass.Reportf(call.Pos(), "%s names no requirement", name)

		return
	}

	for _, arg := range spec.ClaimIDs(name, call.Args) {
		if lit, ok := arg.(*ast.BasicLit); !ok || lit.Kind != token.STRING {
			pass.Reportf(arg.Pos(),
				"requirement id is not a string literal; the requirement gate reads these "+
					"without type information and cannot see through a constant or variable")
		}
	}

	if name == "Satisfies" {
		checkOrder(pass, file, call)
	}
}

// checkOrder holds the ordering rule stated on spec.Satisfies. The enclosing
// function is the innermost one: a subtest's claim rides on its own t, which
// the parent's cleanups cannot reach.
func checkOrder(pass *analysis.Pass, file *ast.File, call *ast.CallExpr) {
	body := innermostBody(file, call.Pos())
	if body == nil {
		return
	}

	for _, stmt := range body.List {
		if call.Pos() >= stmt.Pos() && call.Pos() <= stmt.End() {
			return
		}

		if earlier := firstCall(stmt); earlier != nil {
			pass.Reportf(call.Pos(),
				"Satisfies must be the first statement; %s(...) runs before it, and a cleanup it "+
					"registers would run after the claim and could not withhold it",
				types.ExprString(earlier.Fun))

			return
		}
	}
}

// inFuncDecl reports whether pos lies in a top-level function, which is all
// the requirement gate walks: a literal in a package-level var is out of reach.
func inFuncDecl(file *ast.File, pos token.Pos) bool {
	for _, decl := range file.Decls {
		if _, ok := decl.(*ast.FuncDecl); ok && decl.Pos() <= pos && pos <= decl.End() {
			return true
		}
	}

	return false
}

// innermostBody returns the body of the tightest function enclosing pos.
func innermostBody(file *ast.File, pos token.Pos) *ast.BlockStmt {
	var found *ast.BlockStmt

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil || pos < n.Pos() || pos > n.End() {
			return false
		}

		switch fn := n.(type) {
		case *ast.FuncDecl:
			found = fn.Body
		case *ast.FuncLit:
			found = fn.Body
		}

		return true
	})

	return found
}

// firstCall returns the call a statement makes, ignoring t.Helper(), which
// registers nothing.
func firstCall(stmt ast.Stmt) *ast.CallExpr {
	var found *ast.CallExpr

	ast.Inspect(stmt, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found != nil {
			return true
		}

		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Helper" {
			return true
		}

		found = call

		return false
	})

	return found
}
