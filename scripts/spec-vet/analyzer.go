package main

import (
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const specPath = "github.com/radiergummi/cetacean/internal/spec"

// Analyzer enforces how a test claims a requirement. scripts/spec-gate reads
// the same calls with go/parser and no type information, so two of these rules
// exist for its benefit rather than the compiler's.
var Analyzer = &analysis.Analyzer{
	Name: "specclaim",
	Doc:  "check that spec.Satisfies is called the way the requirement gate can read it",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	// The defining package's own tests call Satisfies to exercise it, not to
	// claim through it, and the gate cannot read them anyway: it matches a
	// spec. selector, which a call from inside the package does not have.
	if strings.TrimSuffix(pass.Pkg.Path(), "_test") == specPath {
		return nil, nil
	}

	for _, file := range pass.Files {
		checkImport(pass, file)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isSatisfies(pass, call) {
				return true
			}

			checkCall(pass, file, call)

			return true
		})
	}

	return nil, nil
}

// isSatisfies resolves the callee rather than matching its spelling, which is
// what the parser-based scan cannot do.
func isSatisfies(pass *analysis.Pass, call *ast.CallExpr) bool {
	var name *ast.Ident

	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		name = fun.Sel
	case *ast.Ident:
		name = fun
	default:
		return false
	}

	fn, ok := pass.TypesInfo.Uses[name].(*types.Func)
	if !ok || fn.Name() != "Satisfies" || fn.Pkg() == nil {
		return false
	}

	return fn.Pkg().Path() == specPath
}

// checkImport holds the no-alias rule. Nothing here needs it — the callee is
// resolved by type — but scripts/spec-gate matches the selector name, so an
// alias would make every claim in the file invisible to the gate.
func checkImport(pass *analysis.Pass, file *ast.File) {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != specPath {
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

func checkCall(pass *analysis.Pass, file *ast.File, call *ast.CallExpr) {
	if !strings.HasSuffix(pass.Fset.Position(call.Pos()).Filename, "_test.go") {
		pass.Reportf(call.Pos(),
			"Satisfies is called outside a _test.go file; internal/spec imports testing, "+
				"and nothing shipped may link it")
	}

	if len(call.Args) < 2 {
		pass.Reportf(call.Pos(), "Satisfies names no requirement")

		return
	}

	for _, arg := range call.Args[1:] {
		if lit, ok := arg.(*ast.BasicLit); !ok || lit.Kind != token.STRING {
			pass.Reportf(arg.Pos(),
				"requirement id is not a string literal; the requirement gate reads these "+
					"without type information and cannot see through a constant or variable")
		}
	}

	checkOrder(pass, file, call)
}

// checkOrder refuses a claim that is not the first thing its function does.
// Cleanups run last-registered-first, so a helper called earlier registers one
// that runs after the claim's own and cannot withhold it when that helper
// fails. The enclosing function is the innermost one: a subtest's claim rides
// on the subtest's own t, which the parent's cleanups cannot reach.
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
				"Satisfies must be the first statement; %s runs before it, and a cleanup it "+
					"registers would run after the claim and could not withhold it",
				render(pass.Fset, earlier))

			return
		}
	}
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

func render(fset *token.FileSet, call *ast.CallExpr) string {
	var b strings.Builder

	if err := printer.Fprint(&b, fset, call.Fun); err != nil {
		return "an earlier call"
	}

	return b.String() + "(...)"
}
