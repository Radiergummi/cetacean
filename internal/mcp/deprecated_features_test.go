package mcp

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// deprecatedAPIs are the mcp-go identifiers through which a server would take
// up each feature the 2026-07-28 revision deprecates.
var deprecatedAPIs = map[string][]string{
	"roots": {"WithRoots", "RequestRoots", "NewRootsInputRequest", "RootsResponse",
		"MethodListRoots"},
	"sampling": {"EnableSampling", "RequestSampling", "NewSamplingInputRequest",
		"SamplingResponse", "MethodSamplingCreateMessage", "WithInProcessSamplingHandler"},
	"logging": {"WithLogging", "SendLogMessageToClient", "SendLogMessageToSpecificClient",
		"NewLoggingMessageNotification"},
}

// deprecatedMethods are the deprecatedAPIs called on a value rather than on
// the package, *MCPServer's and the sessions', so no import alias qualifies them.
var deprecatedMethods = map[string]bool{
	"EnableSampling": true, "RequestSampling": true, "RequestRoots": true,
	"SendLogMessageToClient": true, "SendLogMessageToSpecificClient": true,
}

// deprecatedUses maps each feature to every reference the module's shipped
// code makes to its mcp-go API, as file:line.
var deprecatedUses = sync.OnceValues(func() (map[string][]string, error) {
	feature := map[string]string{}

	for name, apis := range deprecatedAPIs {
		for _, api := range apis {
			feature[api] = name
		}
	}

	uses := map[string][]string{}
	root := filepath.Join("..", "..")

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", ".claude", "frontend", "website", "testdata":
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()

		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}

		aliases := map[string]bool{}

		for _, imp := range file.Imports {
			importPath, _ := strconv.Unquote(imp.Path.Value)
			if !strings.HasPrefix(importPath, "github.com/mark3labs/mcp-go/") {
				continue
			}

			name := filepath.Base(importPath)
			if imp.Name != nil {
				name = imp.Name.Name
			}

			aliases[name] = true
		}

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			name, listed := feature[sel.Sel.Name]
			if !listed {
				return true
			}

			if pkg, ok := sel.X.(*ast.Ident); (ok && aliases[pkg.Name]) ||
				deprecatedMethods[sel.Sel.Name] {
				uses[name] = append(uses[name], fset.Position(sel.Pos()).String())
			}

			return true
		})

		return nil
	})

	return uses, err
})

func usesOfDeprecated(t *testing.T, feature string) []string {
	t.Helper()

	uses, err := deprecatedUses()
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}

	return uses[feature]
}

func TestDeprecatedFeaturesAreNotAdopted(t *testing.T) {
	t.Run("roots", func(t *testing.T) {
		spec.Satisfies(t, "mcp/sep-2577/roots-not-adopted")

		if uses := usesOfDeprecated(t, "roots"); len(uses) > 0 {
			t.Errorf("the server asks the client for roots: %s", strings.Join(uses, ", "))
		}
	})

	t.Run("sampling", func(t *testing.T) {
		spec.Satisfies(t, "mcp/sep-2577/sampling-not-adopted")

		if uses := usesOfDeprecated(t, "sampling"); len(uses) > 0 {
			t.Errorf("the server asks the client to sample: %s", strings.Join(uses, ", "))
		}
	})

	t.Run("logging", func(t *testing.T) {
		spec.Satisfies(t, "mcp/sep-2577/logging-not-adopted")

		if uses := usesOfDeprecated(t, "logging"); len(uses) > 0 {
			t.Errorf("the server sends MCP log messages: %s", strings.Join(uses, ", "))
		}

		_, env := mcpModern(t, newTestServer(t).Handler(), 1, "server/discover", `{}`)
		if env.Error != nil {
			t.Fatalf("server/discover error: %+v", env.Error)
		}

		var result struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		}
		if err := json.Unmarshal(env.Result, &result); err != nil {
			t.Fatalf("decode server/discover: %v", err)
		}

		if _, declared := result.Capabilities["logging"]; declared {
			t.Errorf("server/discover declares the logging capability: %s", env.Result)
		}
	})
}
