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
				lit, ok := value.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}

				if raw, err := strconv.Unquote(lit.Value); err == nil {
					into[name.Name] = raw
				}
			}
		}
	}
}
