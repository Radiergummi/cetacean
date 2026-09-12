package contract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// routerSource is the file the inventory is parsed from, relative to this
// package's directory.
const routerSource = "../api/router.go"

// Route is one pattern registered on the router's mux.
type Route struct {
	// Method is the HTTP method the pattern names. Empty when the
	// registration omits one, which ServeMux reads as "any method".
	Method string

	// Pattern is the path pattern with the method stripped: "/services/{id}".
	Pattern string
}

func (r Route) String() string {
	if r.Method == "" {
		return r.Pattern
	}

	return r.Method + " " + r.Pattern
}

var (
	routesOnce sync.Once
	routes     []Route
	routesErr  error
)

// Routes returns every route registered in internal/api/router.go, sorted and
// deduplicated. It reads the source because ServeMux cannot enumerate its
// patterns, and because the source is what a reviewer edits.
func Routes() ([]Route, error) {
	routesOnce.Do(func() {
		routes, routesErr = parseRoutesFile(routerSource)
	})

	return routes, routesErr
}

func parseRoutesFile(path string) ([]Route, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return collectRoutes(fset, file, path)
}

// parseRoutesFromSource parses routes out of source text rather than a file. It
// exists so the parser's own failure modes can be tested without a fixture file
// on disk.
func parseRoutesFromSource(name, source string) ([]Route, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, name, source, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}

	return collectRoutes(fset, file, name)
}

func collectRoutes(fset *token.FileSet, file *ast.File, name string) ([]Route, error) {
	var (
		found []Route
		bad   []string
		seen  = map[string]bool{}

		// ancestors tracks the current node's ancestor chain, including
		// itself as the last element. ast.Inspect calls the callback with
		// nil once a node's children are all visited, which is the signal to
		// pop — see https://pkg.go.dev/go/ast#Inspect.
		ancestors []ast.Node
	)

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			ancestors = ancestors[:len(ancestors)-1]

			return false
		}

		ancestors = append(ancestors, n)

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc" {
			return true
		}

		// Only registrations on the mux. Handle/HandleFunc on anything else
		// (an inner mux built for a sub-tree, a third-party router) is not
		// part of this surface.
		recv, ok := sel.X.(*ast.Ident)
		if !ok || recv.Name != "mux" {
			return true
		}

		if len(call.Args) == 0 {
			return true
		}

		// One form builds the pattern from a literal prefix and a range
		// variable over an inline slice literal. Still fully determined by the
		// source, so it resolves against the call's own enclosing loop and
		// never another reusing the variable name.
		patterns, ok := resolvePatternLiterals(call.Args[0], enclosingRangeStmt(ancestors))
		if !ok {
			// A computed pattern is invisible to this inventory, so the
			// inventory must refuse to be quietly incomplete.
			bad = append(bad, fmt.Sprintf(
				"%s: %s.%s with a non-literal pattern",
				fset.Position(call.Pos()), recv.Name, sel.Sel.Name,
			))

			return true
		}

		for _, raw := range patterns {
			route := splitPattern(raw)
			if seen[route.String()] {
				continue
			}

			seen[route.String()] = true
			found = append(found, route)
		}

		return true
	})

	if len(bad) > 0 {
		return nil, fmt.Errorf(
			"%s registers routes this inventory cannot see:\n  %s",
			name, strings.Join(bad, "\n  "),
		)
	}

	slices.SortFunc(found, func(a, b Route) int {
		return strings.Compare(a.String(), b.String())
	})

	return found, nil
}

// resolvePatternLiterals returns the pattern string(s) a registration's first
// argument evaluates to, or false if it is not fully determined by literals: a
// bare literal yields one, a literal prefix over an inline slice yields one per
// element. No suffix form, because nothing in router.go uses it.
func resolvePatternLiterals(expr ast.Expr, enclosing *ast.RangeStmt) ([]string, bool) {
	if s, ok := stringLiteralValue(expr); ok {
		return []string{s}, true
	}

	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return nil, false
	}

	prefix, ok := stringLiteralValue(bin.X)
	if !ok {
		return nil, false
	}

	name, ok := identName(bin.Y)
	if !ok {
		return nil, false
	}

	elems, ok := rangeStringElements(enclosing, name)
	if !ok {
		return nil, false
	}

	patterns := make([]string, len(elems))
	for i, elem := range elems {
		patterns[i] = prefix + elem
	}

	return patterns, true
}

func stringLiteralValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}

	raw, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}

	return raw, true
}

func identName(expr ast.Expr) (string, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}

	return ident.Name, true
}

// enclosingRangeStmt returns the innermost *ast.RangeStmt in ancestors, or nil
// if the current node (the last element) is not lexically inside a range
// loop. ancestors includes the current node itself, so the search starts one
// below it.
func enclosingRangeStmt(ancestors []ast.Node) *ast.RangeStmt {
	for i := len(ancestors) - 2; i >= 0; i-- {
		if rs, ok := ancestors[i].(*ast.RangeStmt); ok {
			return rs
		}
	}

	return nil
}

// rangeStringElements returns the literal elements of rs's range expression
// when rs ranges over an inline []string{...} literal with the given loop
// variable name and every element is a string literal. rs may be nil (no
// enclosing loop), which is reported as not found rather than a panic.
func rangeStringElements(rs *ast.RangeStmt, name string) ([]string, bool) {
	if rs == nil {
		return nil, false
	}

	valueIdent, ok := rs.Value.(*ast.Ident)
	if !ok || valueIdent.Name != name {
		return nil, false
	}

	composite, ok := rs.X.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}

	arrayType, ok := composite.Type.(*ast.ArrayType)
	if !ok {
		return nil, false
	}

	elemIdent, ok := arrayType.Elt.(*ast.Ident)
	if !ok || elemIdent.Name != "string" {
		return nil, false
	}

	elems := make([]string, 0, len(composite.Elts))

	for _, elt := range composite.Elts {
		s, ok := stringLiteralValue(elt)
		if !ok {
			return nil, false
		}

		elems = append(elems, s)
	}

	return elems, true
}

// splitPattern separates the optional method from the path in a ServeMux
// pattern. "GET /nodes" yields {GET, /nodes}; "/" yields {"", "/"}.
func splitPattern(pattern string) Route {
	method, path, found := strings.Cut(pattern, " ")
	if !found {
		return Route{Pattern: pattern}
	}

	return Route{Method: method, Pattern: strings.TrimSpace(path)}
}
