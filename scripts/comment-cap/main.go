// Command comment-cap fails on a Go comment block over four lines, the cap
// CLAUDE.md sets. A block is a run of comments with no blank line between;
// directives such as //go:embed and //nolint do not count toward it.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const maxLines = 4

type block struct {
	line, lines int
}

func main() {
	failed := 0

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "frontend", "website", "vendor", ".git", ".claude", "testdata":
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		fset := token.NewFileSet()

		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}

		for _, b := range overlong(fset, file) {
			fmt.Fprintf(os.Stderr, "%s:%d: comment block of %d lines; the cap is %d\n",
				path, b.line, b.lines, maxLines)
			failed++
		}

		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if failed > 0 {
		os.Exit(1)
	}
}

// overlong returns the file's comment blocks past the cap, none for a
// generated file.
func overlong(fset *token.FileSet, file *ast.File) []block {
	if ast.IsGenerated(file) {
		return nil
	}

	var found []block

	for _, group := range file.Comments {
		lines := 0

		for i, c := range group.List {
			// gofmt sets a directive off with a blank comment line.
			if isDirective(c.Text) ||
				c.Text == "//" && i+1 < len(group.List) && isDirective(group.List[i+1].Text) {
				continue
			}

			lines += strings.Count(c.Text, "\n") + 1
		}

		if lines > maxLines {
			found = append(found, block{fset.Position(group.Pos()).Line, lines})
		}
	}

	return found
}

func isDirective(text string) bool {
	for _, prefix := range []string{"//go:", "//nolint", "//line ", "//export "} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}

	return false
}
