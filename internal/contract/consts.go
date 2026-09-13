package contract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// packageStringConsts returns every package-level string constant declared
// beside the given source file. A pattern built from one — "GET "+profilePath —
// is as fully determined as a literal, and refusing it would report the route
// as invisible.
func packageStringConsts(path string) map[string]string {
	dir := filepath.Dir(path)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	fset := token.NewFileSet()
	values := map[string]string{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			continue
		}

		collectStringConsts(file, values)
	}

	return values
}

// fileStringConsts is the same for one parsed file, which is all a parser
// driven from source text has.
func fileStringConsts(file *ast.File) map[string]string {
	values := map[string]string{}
	collectStringConsts(file, values)

	return values
}

func collectStringConsts(file *ast.File, into map[string]string) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != len(value.Values) {
				continue
			}

			for i, name := range value.Names {
				if raw, ok := constStringValue(value.Values[i], into); ok {
					into[name.Name] = raw
				}
			}
		}
	}
}

// constStringValue evaluates a constant's expression against the constants
// already collected: a literal, a name standing for one, or a concatenation of
// those — asyncAPIYAMLPath is asyncAPIPath + ".yaml". Declaration order is
// enough to resolve it, since a constant cannot precede the one it is built on.
func constStringValue(expr ast.Expr, known map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}

		raw, err := strconv.Unquote(e.Value)

		return raw, err == nil

	case *ast.Ident:
		value, ok := known[e.Name]

		return value, ok

	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}

		left, ok := constStringValue(e.X, known)
		if !ok {
			return "", false
		}

		right, ok := constStringValue(e.Y, known)
		if !ok {
			return "", false
		}

		return left + right, true

	default:
		return "", false
	}
}
