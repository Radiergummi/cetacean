package contract

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/metrics"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata")

const (
	knownEnvSource = "../config/knownenv_gen.go"
	widgetsBuild   = "../../frontend/dist-widgets"
)

// assertGolden compares one surface's listing with its committed golden file.
// The files are the v1 compatibility surface: a line that disappears is a
// removal or rename someone must have meant.
func assertGolden(t *testing.T, name string, lines []string) {
	t.Helper()

	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create it)", err)
	}

	if got == string(want) {
		return
	}

	gone, added := lineDiff(strings.Split(string(want), "\n"), lines)
	t.Errorf(
		"%s no longer matches the surface.\nmissing:\n  %s\nnew:\n  %s\n\nA missing line "+
			"breaks a 1.x caller. If the change is intended, run\n  go test "+
			"./internal/contract -run TestGolden -update\nand commit the file with it.",
		path, listOrNone(gone), listOrNone(added),
	)
}

func listOrNone(lines []string) string {
	if len(lines) == 0 {
		return "(none)"
	}

	return strings.Join(lines, "\n  ")
}

func lineDiff(want, got []string) (gone, added []string) {
	for _, line := range want {
		if line != "" && !slices.Contains(got, line) {
			gone = append(gone, line)
		}
	}

	for _, line := range got {
		if !slices.Contains(want, line) {
			added = append(added, line)
		}
	}

	return gone, added
}

func TestGoldenRoutes(t *testing.T) {
	routes, err := Routes()
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}

	reps, err := Representations()
	if err != nil {
		t.Fatalf("Representations: %v", err)
	}

	lines := make([]string, 0, len(routes))

	for _, route := range routes {
		line := route.String()

		if served := reps[route.String()]; len(served) > 0 {
			names := make([]string, len(served))
			for i, r := range served {
				names[i] = string(r)
			}

			line += "  " + strings.Join(names, " ")
		}

		lines = append(lines, line)
	}

	assertGolden(t, "routes", lines)
}

func TestGoldenMCP(t *testing.T) {
	widgets := os.DirFS(widgetsBuild)
	if entries, err := os.ReadDir(widgetsBuild); err != nil || len(entries) == 0 {
		t.Fatalf(
			"%s is empty or missing; build it with `pnpm --filter frontend "+
				"build:widgets`, or the tool-to-widget mapping reads as removed",
			widgetsBuild,
		)
	}

	w := NewWorldWithWidgets(t, widgets)
	catalog := MCPCatalog(t, w)

	var lines []string

	for _, tool := range catalog.Tools {
		line := "tool " + tool.Name

		var meta struct {
			UI struct {
				ResourceURI string `json:"resourceUri"`
			} `json:"ui"`
		}

		if len(tool.Meta) > 0 {
			if err := json.Unmarshal(tool.Meta, &meta); err != nil {
				t.Fatalf("decode %s _meta: %v", tool.Name, err)
			}
		}

		if meta.UI.ResourceURI != "" {
			line += " -> " + meta.UI.ResourceURI
		}

		lines = append(lines, line)

		var schema jsonSchema
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("decode %s input schema: %v", tool.Name, err)
		}

		lines = append(lines, schema.arguments(tool.Name+".")...)
	}

	for _, uri := range catalog.ResourceTemplates {
		lines = append(lines, "resource-template "+uri)
	}

	for _, uri := range staticResources(t, w) {
		lines = append(lines, "resource "+uri)
	}

	for _, name := range catalog.Prompts {
		lines = append(lines, "prompt "+name)
	}

	assertGolden(t, "mcp", lines)
}

func staticResources(t *testing.T, w *World) []string {
	t.Helper()

	result, rpcErr := w.MCP(t, "ops", "resources/list", nil)
	if rpcErr != nil {
		t.Fatalf("resources/list: %s", rpcErr.Message)
	}

	var listed struct {
		Resources []struct {
			URI string `json:"uri"`
		} `json:"resources"`
	}

	if err := json.Unmarshal(result, &listed); err != nil {
		t.Fatalf("decode resources/list: %v", err)
	}

	uris := make([]string, len(listed.Resources))
	for i, r := range listed.Resources {
		uris[i] = r.URI
	}

	slices.Sort(uris)

	return uris
}

// jsonSchema is the part of an input schema a caller's arguments depend on.
type jsonSchema struct {
	Type       json.RawMessage        `json:"type"`
	Enum       []any                  `json:"enum"`
	Properties map[string]*jsonSchema `json:"properties"`
	Items      *jsonSchema            `json:"items"`
	Required   []string               `json:"required"`
}

// arguments lists one line per property, a nested one under its parent's
// path and an array's elements as name[].
func (s *jsonSchema) arguments(prefix string) []string {
	var lines []string

	for _, name := range slices.Sorted(maps.Keys(s.Properties)) {
		property := s.Properties[name]

		line := "  " + prefix + name + ": " + property.describe()
		if slices.Contains(s.Required, name) {
			line += " required"
		}

		lines = append(lines, line)

		if property.Items != nil {
			lines = append(lines, property.Items.arguments(prefix+name+"[].")...)
		} else {
			lines = append(lines, property.arguments(prefix+name+".")...)
		}
	}

	return lines
}

func (s *jsonSchema) describe() string {
	kind := "any"

	if len(s.Type) > 0 {
		var single string
		if json.Unmarshal(s.Type, &single) == nil {
			kind = single
		} else {
			var several []string
			if json.Unmarshal(s.Type, &several) == nil {
				kind = strings.Join(several, "|")
			}
		}
	}

	if kind == "array" && s.Items != nil {
		kind = s.Items.describe() + "[]"
	}

	if len(s.Enum) > 0 {
		values := make([]string, len(s.Enum))
		for i, v := range s.Enum {
			values[i] = fmt.Sprint(v)
		}

		slices.Sort(values)
		kind += " enum(" + strings.Join(values, ",") + ")"
	}

	return kind
}

func TestGoldenConfiguration(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), knownEnvSource, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", knownEnvSource, err)
	}

	var names []string

	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}

		if name, ok := stringLiteralValue(kv.Key); ok {
			names = append(names, name)
		}

		return false
	})

	if len(names) == 0 {
		t.Fatalf("%s lists no settings; the parser no longer reads its shape", knownEnvSource)
	}

	slices.Sort(names)

	assertGolden(t, "configuration", names)
}

func TestGoldenErrorCodes(t *testing.T) {
	defs := problem.Defs()
	lines := make([]string, len(defs))

	for i, def := range defs {
		lines[i] = def.Code + " " + strconv.Itoa(def.Status)
	}

	assertGolden(t, "errors", lines)
}

var descPattern = regexp.MustCompile(`^Desc\{fqName: "([^"]+)", .*variableLabels: \{([^}]*)\}\}$`)

func TestGoldenMetrics(t *testing.T) {
	descs := make(chan *prometheus.Desc)

	go func() {
		metrics.Registry.Describe(descs)
		close(descs)
	}()

	var lines []string

	for desc := range descs {
		match := descPattern.FindStringSubmatch(desc.String())
		if match == nil {
			t.Fatalf("cannot read the metric descriptor %s", desc)
		}

		labels := strings.Split(match[2], ",")
		slices.Sort(labels)
		lines = append(lines, match[1]+"{"+strings.Join(labels, ",")+"}")
	}

	slices.Sort(lines)

	assertGolden(t, "metrics", lines)
}
