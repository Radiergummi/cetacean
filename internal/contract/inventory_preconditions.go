package contract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"sync"
)

var (
	precondOnce sync.Once
	preconds    []Route
	precondErr  error
)

// PreconditionedRoutes returns every route whose middleware chain includes the
// If-Match precondition, sorted and deduplicated.
//
// It reads router.go for the same reason Routes does: the chain a route is
// registered with is what a reviewer edits, and an inventory derived from it
// fails where the drift is introduced. A route reaches the middleware either
// directly — Append(h.precond(...)) in the registration — or through a chain
// variable built once and registered twice, which is how the two healthcheck
// methods share one representation.
func PreconditionedRoutes() ([]Route, error) {
	precondOnce.Do(func() {
		fset := token.NewFileSet()

		file, err := parser.ParseFile(fset, routerSource, nil, 0)
		if err != nil {
			precondErr = fmt.Errorf("parse %s: %w", routerSource, err)

			return
		}

		preconds, precondErr = collectPreconditioned(fset, file, routerSource)
	})

	return preconds, precondErr
}

// parsePreconditionedFromSource reads preconditions out of source text rather
// than a file, so the parser's own failure modes can be tested without a
// fixture on disk.
func parsePreconditionedFromSource(name, source string) ([]Route, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, name, source, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}

	return collectPreconditioned(fset, file, name)
}

// collectPreconditioned finds the registrations that carry h.precond. It runs
// in two passes because a chain variable may be defined after nothing and used
// later: the first resolves which identifiers carry the middleware, the second
// reads the registrations.
func collectPreconditioned(fset *token.FileSet, file *ast.File, name string) ([]Route, error) {
	sites := precondCallSites(file)
	if len(sites) == 0 {
		return nil, fmt.Errorf(
			"%s registers no If-Match preconditions; the parser is looking for the "+
				"wrong call", name,
		)
	}

	carriers := precondCarrierVars(file)

	var (
		found   []Route
		bad     []string
		seen    = map[string]bool{}
		reached = map[token.Pos]bool{}

		ancestors []ast.Node
	)

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			ancestors = ancestors[:len(ancestors)-1]

			return false
		}

		ancestors = append(ancestors, n)

		call, ok := muxRegistration(n)
		if !ok {
			return true
		}

		covered := coveredSites(call.Args[1:], carriers)
		if len(covered) == 0 {
			return true
		}

		patterns, ok := resolvePatternLiterals(call.Args[0], enclosingRangeStmt(ancestors))
		if !ok {
			bad = append(bad, fmt.Sprintf(
				"%s: preconditioned registration with a non-literal pattern",
				fset.Position(call.Pos()),
			))

			return true
		}

		for _, pos := range covered {
			reached[pos] = true
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

	for _, pos := range sites {
		if !reached[pos] {
			bad = append(bad, fmt.Sprintf(
				"%s: h.precond call reaches no registration this inventory can see",
				fset.Position(pos),
			))
		}
	}

	if len(bad) > 0 {
		return nil, fmt.Errorf(
			"%s wires preconditions this inventory cannot see:\n  %s",
			name, strings.Join(bad, "\n  "),
		)
	}

	slices.SortFunc(found, func(a, b Route) int {
		return strings.Compare(a.String(), b.String())
	})

	return found, nil
}

// muxRegistration reports whether n is a mux.Handle/HandleFunc call carrying
// both a pattern and a handler.
func muxRegistration(n ast.Node) (*ast.CallExpr, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return nil, false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}

	if sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc" {
		return nil, false
	}

	recv, ok := sel.X.(*ast.Ident)
	if !ok || recv.Name != "mux" {
		return nil, false
	}

	if len(call.Args) < 2 {
		return nil, false
	}

	return call, true
}

// precondCallSites returns the position of every h.precond call in the file.
// They are the denominator the completeness check below works against: one
// that reaches no registration means the parser stopped seeing a wiring form.
func precondCallSites(file *ast.File) []token.Pos {
	var sites []token.Pos

	ast.Inspect(file, func(n ast.Node) bool {
		if isPrecondCall(n) {
			sites = append(sites, n.Pos())
		}

		return true
	})

	return sites
}

func isPrecondCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "precond" {
		return false
	}

	recv, ok := sel.X.(*ast.Ident)

	return ok && recv.Name == "h"
}

// precondCarrierVars maps each identifier assigned a chain that carries the
// middleware to the call sites that chain holds. It repeats to a fixed point so
// a chain built from another chain is followed, rather than reported as an
// unreachable call site.
func precondCarrierVars(file *ast.File) map[string][]token.Pos {
	carriers := map[string][]token.Pos{}

	for {
		grew := false

		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != len(assign.Rhs) {
				return true
			}

			for i, lhs := range assign.Lhs {
				name, ok := identName(lhs)
				if !ok || name == "_" {
					continue
				}

				covered := coveredSites([]ast.Expr{assign.Rhs[i]}, carriers)
				if len(covered) > len(carriers[name]) {
					carriers[name] = covered
					grew = true
				}
			}

			return true
		})

		if !grew {
			return carriers
		}
	}
}

// coveredSites returns the h.precond call sites the given expressions reach,
// either lexically or through a chain variable.
func coveredSites(exprs []ast.Expr, carriers map[string][]token.Pos) []token.Pos {
	var covered []token.Pos

	for _, expr := range exprs {
		ast.Inspect(expr, func(n ast.Node) bool {
			if isPrecondCall(n) {
				covered = append(covered, n.Pos())

				return true
			}

			if ident, ok := n.(*ast.Ident); ok {
				covered = append(covered, carriers[ident.Name]...)
			}

			return true
		})
	}

	slices.Sort(covered)

	return slices.Compact(covered)
}
