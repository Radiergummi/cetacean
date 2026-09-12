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

// Representation names one media form a route can serve. The values match the
// ContentType constants in internal/api/negotiate.go, minus the suffix.
type Representation string

const (
	RepresentationJSON     Representation = "JSON"
	RepresentationHTML     Representation = "HTML"
	RepresentationSSE      Representation = "SSE"
	RepresentationAtom     Representation = "Atom"
	RepresentationJSONFeed Representation = "JSONFeed"
	RepresentationJGF      Representation = "JGF"
	RepresentationGraphML  Representation = "GraphML"
	RepresentationDOT      Representation = "DOT"
	RepresentationCSV      Representation = "CSV"
)

var (
	repsOnce sync.Once
	reps     map[string][]Representation
	repsErr  error
)

// Representations maps each content-negotiated route to the representations its
// registration declares, derived from router.go's two wiring forms: the
// dispatch helpers and the handful negotiating by hand. A feed argument in
// neither shape is an error, not a route reported as serving less than it does.
func Representations() (map[string][]Representation, error) {
	repsOnce.Do(func() {
		fset := token.NewFileSet()

		file, err := parser.ParseFile(fset, routerSource, nil, 0)
		if err != nil {
			repsErr = fmt.Errorf("parse %s: %w", routerSource, err)

			return
		}

		reps, repsErr = collectRepresentations(
			fset, file, routerSource, packageStringConsts(routerSource),
		)
	})

	return reps, repsErr
}

// parseRepresentationsFromSource reads the inventory out of source text
// rather than a file, so the parser's own failure modes can be tested without
// a fixture on disk.
func parseRepresentationsFromSource(name, source string) (map[string][]Representation, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, name, source, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}

	return collectRepresentations(fset, file, name, fileStringConsts(file))
}

func collectRepresentations(
	fset *token.FileSet,
	file *ast.File,
	name string,
	consts map[string]string,
) (map[string][]Representation, error) {
	found := map[string][]Representation{}

	var (
		bad       []string
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

		declared, problems := declaredRepresentations(fset, call.Args[1:])
		bad = append(bad, problems...)

		if len(declared) == 0 {
			return true
		}

		patterns, ok := resolvePatternLiterals(call.Args[0], enclosingRangeStmt(ancestors), consts)
		if !ok {
			bad = append(bad, fmt.Sprintf(
				"%s: content-negotiated registration with a non-literal pattern",
				fset.Position(call.Pos()),
			))

			return true
		}

		for _, raw := range patterns {
			key := splitPattern(raw).String()
			found[key] = mergeRepresentations(found[key], declared)
		}

		return true
	})

	if len(bad) > 0 {
		return nil, fmt.Errorf(
			"%s negotiates content in ways this inventory cannot see:\n  %s",
			name, strings.Join(bad, "\n  "),
		)
	}

	if len(found) == 0 {
		return nil, fmt.Errorf("%s declares no content-negotiated routes at all", name)
	}

	return found, nil
}

// declaredRepresentations resolves what a registration's handler arguments say
// the route can serve.
func declaredRepresentations(
	fset *token.FileSet,
	args []ast.Expr,
) ([]Representation, []string) {
	var (
		declared []Representation
		problems []string
	)

	for _, arg := range args {
		ast.Inspect(arg, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if ok {
				if helper, feeds, isHelper := dispatchHelper(call); isHelper {
					from, problem := helperRepresentations(fset, helper, feeds)
					if problem != "" {
						problems = append(problems, problem)
					}

					declared = mergeRepresentations(declared, from)
				}

				return true
			}

			// A hand-written negotiation names its ContentType constants,
			// which is the only trace it leaves in the registration — and
			// falls back to the SPA, whose identifier is the trace of the
			// HTML the dispatch helpers declare structurally.
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if r, named := representationOf(sel.Sel.Name); named {
					declared = mergeRepresentations(declared, []Representation{r})
				}

				return true
			}

			if ident, ok := n.(*ast.Ident); ok {
				if r, named := representationOf(ident.Name); named {
					declared = mergeRepresentations(declared, []Representation{r})
				}

				if ident.Name == "spa" {
					declared = mergeRepresentations(
						declared, []Representation{RepresentationHTML},
					)
				}
			}

			return true
		})
	}

	return declared, problems
}

// dispatchHelper reports whether call is one of the two dispatch helpers, and
// returns its feedHandlers argument.
func dispatchHelper(call *ast.CallExpr) (name string, feeds ast.Expr, ok bool) {
	ident, isIdent := call.Fun.(*ast.Ident)
	if !isIdent {
		return "", nil, false
	}

	switch ident.Name {
	case "contentNegotiated":
		if len(call.Args) != 3 {
			return ident.Name, nil, true
		}

		return ident.Name, call.Args[1], true
	case "contentNegotiatedWithSSE":
		if len(call.Args) != 4 {
			return ident.Name, nil, true
		}

		return ident.Name, call.Args[2], true
	default:
		return "", nil, false
	}
}

// helperRepresentations resolves the media coverage a dispatch-helper call
// declares. Both helpers always serve JSON and HTML; contentNegotiatedWithSSE
// adds SSE; and the feed formats depend on what the feedHandlers argument
// carries.
func helperRepresentations(
	fset *token.FileSet,
	helper string,
	feeds ast.Expr,
) ([]Representation, string) {
	declared := []Representation{RepresentationJSON, RepresentationHTML}
	if helper == "contentNegotiatedWithSSE" {
		declared = append(declared, RepresentationSSE)
	}

	if feeds == nil {
		return declared, fmt.Sprintf("%s call with an unexpected argument count", helper)
	}

	atom, jsonFeed, csv, ok := feedFields(feeds)
	if !ok {
		return declared, fmt.Sprintf(
			"%s: %s with a feedHandlers argument this inventory cannot read",
			fset.Position(feeds.Pos()), helper,
		)
	}

	if atom {
		declared = append(declared, RepresentationAtom)
	}

	if jsonFeed {
		declared = append(declared, RepresentationJSONFeed)
	}

	if csv {
		declared = append(declared, RepresentationCSV)
	}

	return declared, ""
}

// feedFields reports which feed handlers a feedHandlers argument carries. Both
// builders return both; a composite literal carries whichever keys it names.
func feedFields(expr ast.Expr) (atom, jsonFeed, csv, ok bool) {
	switch e := expr.(type) {
	case *ast.CallExpr:
		sel, isSel := e.Fun.(*ast.SelectorExpr)
		if !isSel {
			return false, false, false, false
		}

		switch sel.Sel.Name {
		case "listFeeds":
			return true, true, true, true
		case "detailFeeds", "searchFeeds":
			return true, true, false, true
		default:
			return false, false, false, false
		}

	case *ast.CompositeLit:
		ident, isIdent := e.Type.(*ast.Ident)
		if !isIdent || ident.Name != "feedHandlers" {
			return false, false, false, false
		}

		for _, elt := range e.Elts {
			kv, isKV := elt.(*ast.KeyValueExpr)
			if !isKV {
				return false, false, false, false
			}

			key, isIdent := kv.Key.(*ast.Ident)
			if !isIdent {
				return false, false, false, false
			}

			switch key.Name {
			case "atom":
				atom = true
			case "jsonFeed":
				jsonFeed = true
			case "csv":
				csv = true
			case "csvParams":
				// Parameterises the CSV link; it declares no representation.
			default:
				return false, false, false, false
			}
		}

		return atom, jsonFeed, csv, true

	default:
		return false, false, false, false
	}
}

// representationOf maps a ContentType constant's name to its representation.
func representationOf(name string) (Representation, bool) {
	trimmed, ok := strings.CutPrefix(name, "ContentType")
	if !ok {
		return "", false
	}

	switch r := Representation(trimmed); r {
	case RepresentationJSON, RepresentationHTML, RepresentationSSE,
		RepresentationAtom, RepresentationJSONFeed, RepresentationJGF,
		RepresentationGraphML, RepresentationDOT, RepresentationCSV:
		return r, true
	default:
		// ContentTypeUnsupported and the context helpers land here.
		return "", false
	}
}

func mergeRepresentations(into, from []Representation) []Representation {
	for _, r := range from {
		if !slices.Contains(into, r) {
			into = append(into, r)
		}
	}

	slices.Sort(into)

	return into
}
