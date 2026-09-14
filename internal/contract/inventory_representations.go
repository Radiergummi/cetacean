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
	RepresentationYAML     Representation = "YAML"
)

var (
	repsOnce sync.Once
	reps     map[string][]Representation
	repsErr  error
)

// Representations maps each content-negotiated route, keyed by Route.String(),
// to the representations its registration declares.
//
// It is derived from router.go's own dispatch wiring, in two forms that have
// to be read together. Most routes are registered through contentNegotiated or
// contentNegotiatedWithSSE, whose media coverage is fixed by the helper and by
// whether its feedHandlers argument carries anything. A few negotiate by hand,
// switching on ContentTypeFromContext; those name their ContentType constants
// lexically, which is what the second form reads.
//
// A dispatch-helper call whose feed argument is in neither recognised shape is
// an error rather than a route quietly reported as serving less than it does.
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

		declared, problems := declaredRepresentations(
			fset, call.Args[1:], enclosingFuncDecl(ancestors),
		)
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
	fn *ast.FuncDecl,
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
					from, problem := helperRepresentations(fset, helper, feeds, fn)
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
	fn *ast.FuncDecl,
) ([]Representation, string) {
	declared := []Representation{RepresentationJSON, RepresentationHTML}
	if helper == "contentNegotiatedWithSSE" {
		declared = append(declared, RepresentationSSE)
	}

	if feeds == nil {
		return declared, fmt.Sprintf("%s call with an unexpected argument count", helper)
	}

	fields, ok := feedFields(feeds, fn)
	if !ok {
		return declared, fmt.Sprintf(
			"%s: %s with a feedHandlers argument this inventory cannot read",
			fset.Position(feeds.Pos()), helper,
		)
	}

	if fields.atom {
		declared = append(declared, RepresentationAtom)
	}

	if fields.jsonFeed {
		declared = append(declared, RepresentationJSONFeed)
	}

	if fields.csv {
		declared = append(declared, RepresentationCSV)
	}

	if fields.yaml {
		declared = append(declared, RepresentationYAML)
	}

	return declared, ""
}

// feedSet reports which feed handlers a feedHandlers value carries.
type feedSet struct {
	atom     bool
	jsonFeed bool
	csv      bool
	yaml     bool
}

// feedFields reads a feedHandlers argument in every shape router.go uses: a
// builder call, a composite literal, or a local variable built from either and
// then given further fields. fn may be nil, which makes a variable unreadable
// rather than a panic.
func feedFields(expr ast.Expr, fn *ast.FuncDecl) (fields feedSet, ok bool) {
	switch e := expr.(type) {
	case *ast.CallExpr:
		sel, isSel := e.Fun.(*ast.SelectorExpr)
		if !isSel {
			return feedSet{}, false
		}

		switch sel.Sel.Name {
		case "listFeeds":
			return feedSet{atom: true, jsonFeed: true, csv: true}, true
		case "detailFeeds", "searchFeeds":
			return feedSet{atom: true, jsonFeed: true}, true
		default:
			return feedSet{}, false
		}

	case *ast.CompositeLit:
		ident, isIdent := e.Type.(*ast.Ident)
		if !isIdent || ident.Name != "feedHandlers" {
			return feedSet{}, false
		}

		for _, elt := range e.Elts {
			kv, isKV := elt.(*ast.KeyValueExpr)
			if !isKV {
				return feedSet{}, false
			}

			key, isIdent := kv.Key.(*ast.Ident)
			if !isIdent || !fields.set(key.Name) {
				return feedSet{}, false
			}
		}

		return fields, true

	case *ast.Ident:
		return feedVariable(e.Name, fn)

	default:
		return feedSet{}, false
	}
}

// set records the field a feedHandlers key names, reporting whether the key is
// one this parser knows.
func (f *feedSet) set(key string) bool {
	switch key {
	case "atom":
		f.atom = true
	case "jsonFeed":
		f.jsonFeed = true
	case "csv":
		f.csv = true
	case "yaml":
		f.yaml = true
	case "csvParams", "queryParams":
		// Parameterise a link; they declare no representation.
	default:
		return false
	}

	return true
}

// feedVariable resolves a feedHandlers local: the value it is assigned, plus
// every field assigned to it afterwards. Assignment order does not matter,
// because a field is only ever set, never cleared.
func feedVariable(name string, fn *ast.FuncDecl) (fields feedSet, ok bool) {
	if fn == nil {
		return feedSet{}, false
	}

	ast.Inspect(fn, func(n ast.Node) bool {
		assign, isAssign := n.(*ast.AssignStmt)
		if !isAssign || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}

		switch lhs := assign.Lhs[0].(type) {
		case *ast.Ident:
			if lhs.Name != name {
				return true
			}

			from, readable := feedFields(assign.Rhs[0], fn)
			if !readable {
				ok = false

				return false
			}

			fields.atom = fields.atom || from.atom
			fields.jsonFeed = fields.jsonFeed || from.jsonFeed
			fields.csv = fields.csv || from.csv
			fields.yaml = fields.yaml || from.yaml
			ok = true

		case *ast.SelectorExpr:
			target, isIdent := lhs.X.(*ast.Ident)
			if !isIdent || target.Name != name {
				return true
			}

			if !fields.set(lhs.Sel.Name) {
				ok = false

				return false
			}
		}

		return true
	})

	return fields, ok
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
		RepresentationGraphML, RepresentationDOT, RepresentationCSV,
		RepresentationYAML:
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
