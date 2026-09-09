package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// These tests hold docs/mcp-tools.mdx against the catalog it documents. The
// page states an operations level, an argument list and a set of accepted
// values for each of 27 tools, a table of prompts and one of widgets, and four
// counts — all typed by hand against this package.
//
// They live here, in `package mcp`, because everything they read is unexported:
// a script outside the package would need toolCatalog, toolDef.tier,
// promptCatalog, promptTier and widgetCatalog exported first, which is a new
// public surface on a domain package for the docs site's sake. Measured over
// the repository's history, the drift these catch arrives from the docs side
// roughly twenty times more often than from Go, and `ci.yml` carries no path
// filter, so `go test` runs on a documentation-only pull request too.
//
// Every rule runs schema-to-doc: a fact the catalog states must appear on the
// page. The reverse would flag every value the prose names in passing, so an
// argument deleted in Go and left in the docs is deliberately not caught. See
// docs/specs/2026-09-09-mcp-catalog-drift-check-design.md.
const (
	catalogPath = "../../docs/mcp-tools.mdx"
	widgetsPath = "../../frontend/src/widgets"
)

var (
	cardPattern = regexp.MustCompile(
		`(?s)<McpTool name="([a-z_]+)" level=\{(\d)}>(.*?)</McpTool>`,
	)
	argumentsPattern = regexp.MustCompile(`(?s)<Fragment slot="arguments">(.*?)</Fragment>`)
	backtickPattern  = regexp.MustCompile("`([^`]+)`")
	parenPattern     = regexp.MustCompile(`\([^()]*\)`)
	// The prose is hand-wrapped, so the sentence may break at any of its spaces.
	countsPattern = regexp.MustCompile(
		`(\d+)\s+resources,\s+(\d+)\s+tools,\s+(\d+)\s+prompts,\s+and\s+(\d+)\s+widgets`,
	)
)

// card is one <McpTool> block: the level it advertises, its arguments fact, and
// its whole body. The two are kept apart because the rules read them at
// different scopes — see documentedNames and documentedValues.
type card struct {
	level     int
	arguments string
	body      string
}

func catalogDoc(t *testing.T) string {
	t.Helper()

	content, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("read %s: %v", catalogPath, err)
	}

	return string(content)
}

func toolCards(t *testing.T) map[string]card {
	t.Helper()

	doc := catalogDoc(t)
	cards := map[string]card{}

	for _, match := range cardPattern.FindAllStringSubmatch(doc, -1) {
		name, rawLevel, body := match[1], match[2], match[3]

		level, err := strconv.Atoi(rawLevel)
		if err != nil {
			t.Fatalf("card %q: level %q is not a number", name, rawLevel)
		}

		arguments := argumentsPattern.FindStringSubmatch(body)
		if arguments == nil {
			t.Fatalf("card %q has no arguments fact", name)
		}

		cards[name] = card{level: level, arguments: arguments[1], body: body}
	}

	if len(cards) == 0 {
		t.Fatalf("no <McpTool> cards found in %s — the page's markup has changed", catalogPath)
	}

	return cards
}

// documentedNames reads the arguments fact with parenthesised groups removed.
// Both narrowings are load-bearing. Reading the whole card would let a property
// named after one of update_service's ten sections pass on that card's section
// table without being documented as an argument; keeping parentheses would let
// a new `service` argument on get_metrics pass on an enum value of `target`.
func documentedNames(c card) map[string]bool {
	return backticked(parenPattern.ReplaceAllString(c.arguments, ""))
}

// documentedValues reads the whole card, because a value set is legitimately
// documented outside the arguments fact: update_service's ten sections are a
// table in the card body, which is the right place for them.
func documentedValues(c card) map[string]bool {
	return backticked(c.body)
}

func backticked(text string) map[string]bool {
	found := map[string]bool{}

	for _, match := range backtickPattern.FindAllStringSubmatch(text, -1) {
		found[match[1]] = true
	}

	return found
}

// schemaEnum returns the values a property accepts, or nil where it declares
// none. mcp-go's Enum option writes a []string, but the schema is a
// map[string]any that any option may have written to, so both shapes are read.
func schemaEnum(property any) []string {
	fields, ok := property.(map[string]any)
	if !ok {
		return nil
	}

	switch values := fields["enum"].(type) {
	case []string:
		return values
	case []any:
		var out []string

		for _, value := range values {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}

		return out
	}

	return nil
}

func catalogServer(t *testing.T) *Server {
	t.Helper()

	return newResourceTestServer(t, cache.New(nil))
}

// Rules 1 and 2: the page documents every tool, documents no tool that does not
// exist, and gives each the level that actually gates it.
func TestEveryToolHasACardAtItsTier(t *testing.T) {
	srv := catalogServer(t)
	cards := toolCards(t)
	catalog := srv.toolCatalog()

	for _, def := range catalog {
		c, ok := cards[def.tool.Name]
		if !ok {
			t.Errorf("tool %q has no card in %s", def.tool.Name, catalogPath)

			continue
		}

		if config.OperationsLevel(c.level) != def.tier {
			t.Errorf(
				"tool %q: card says level %d, catalog registers it at %d",
				def.tool.Name, c.level, def.tier,
			)
		}
	}

	for name := range cards {
		if !slices.ContainsFunc(catalog, func(def toolDef) bool { return def.tool.Name == name }) {
			t.Errorf("card %q documents a tool the catalog does not register", name)
		}
	}
}

// Rule 3: every argument a tool accepts is named in its card's arguments fact.
func TestEveryToolArgumentIsDocumented(t *testing.T) {
	srv := catalogServer(t)
	cards := toolCards(t)

	for _, def := range srv.toolCatalog() {
		c, ok := cards[def.tool.Name]
		if !ok {
			continue // reported by TestEveryToolHasACardAtItsTier
		}

		named := documentedNames(c)

		for property := range def.tool.InputSchema.Properties {
			if !named[property] {
				t.Errorf(
					"tool %q accepts %q, which its arguments fact does not name",
					def.tool.Name, property,
				)
			}
		}
	}
}

// Rule 4: every value a tool's schema restricts an argument to is named
// somewhere in its card. update_service's `section` is the case this exists
// for: its enum is derived from serviceSectionWriters' keys, so an eleventh
// section is a one-key Go change that invalidates a ten-row table on the page.
func TestEverySchemaEnumValueIsDocumented(t *testing.T) {
	srv := catalogServer(t)
	cards := toolCards(t)

	for _, def := range srv.toolCatalog() {
		c, ok := cards[def.tool.Name]
		if !ok {
			continue
		}

		documented := documentedValues(c)

		for property, schema := range def.tool.InputSchema.Properties {
			for _, value := range schemaEnum(schema) {
				if !documented[value] {
					t.Errorf(
						"tool %q: %q accepts %q, which its card never names",
						def.tool.Name, property, value,
					)
				}
			}
		}
	}
}

// markdownTable returns the rows of the first table whose header contains every
// given heading, with the header and the separator dropped.
func markdownTable(t *testing.T, headings ...string) [][]string {
	t.Helper()

	var rows [][]string

	for line := range strings.SplitSeq(catalogDoc(t), "\n") {
		trimmed := strings.TrimSpace(line)

		if !strings.HasPrefix(trimmed, "|") {
			if len(rows) > 0 {
				break // the table ended
			}

			continue
		}

		cells := tableCells(trimmed)

		if len(rows) == 0 {
			if !containsAll(cells, headings) {
				continue
			}

			rows = append(rows, cells)

			continue
		}

		if strings.Trim(strings.Join(cells, ""), "-: ") == "" {
			continue // the separator row
		}

		rows = append(rows, cells)
	}

	if len(rows) < 2 {
		t.Fatalf("no table with headings %v found in %s", headings, catalogPath)
	}

	return rows[1:]
}

func tableCells(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	cells := make([]string, 0, len(parts))

	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}

	return cells
}

func containsAll(cells, wanted []string) bool {
	for _, want := range wanted {
		if !slices.Contains(cells, want) {
			return false
		}
	}

	return true
}

// bare strips the backticks a table cell wraps an identifier in.
func bare(cell string) string {
	return strings.Trim(cell, "`")
}

// Rule 5a: the prompts table's mechanical columns — the tier promptTier derives
// from the tools a prompt drives, its argument, and the resource types it needs
// read access to — match the catalog. Only the last column is prose.
func TestPromptsTableMatchesTheCatalog(t *testing.T) {
	srv := catalogServer(t)
	tiers := srv.toolTiers()
	rows := markdownTable(t, "Prompt", "Level", "Argument", "Reads")

	documented := map[string][]string{}

	for _, row := range rows {
		documented[bare(row[0])] = row
	}

	for _, def := range promptCatalog() {
		row, ok := documented[def.prompt.Name]
		if !ok {
			t.Errorf("prompt %q has no row in the prompts table", def.prompt.Name)

			continue
		}

		if want := strconv.Itoa(int(promptTier(def, tiers))); row[1] != want {
			t.Errorf(
				"prompt %q: table says level %s, catalog derives %s",
				def.prompt.Name,
				row[1],
				want,
			)
		}

		var arguments []string

		for _, argument := range def.prompt.Arguments {
			arguments = append(arguments, argument.Name)
		}

		if got := namedInCell(row[2]); !slices.Equal(got, arguments) {
			t.Errorf(
				"prompt %q: table lists arguments %v, catalog declares %v",
				def.prompt.Name,
				got,
				arguments,
			)
		}

		if got := namedInCell(row[3]); !slices.Equal(got, def.reads) {
			t.Errorf(
				"prompt %q: table lists reads %v, catalog declares %v",
				def.prompt.Name,
				got,
				def.reads,
			)
		}
	}

	for name := range documented {
		if !slices.ContainsFunc(
			promptCatalog(),
			func(def promptDef) bool { return def.prompt.Name == name },
		) {
			t.Errorf("prompts table lists %q, which the catalog does not register", name)
		}
	}
}

// namedInCell reads a comma-separated table cell, treating "none" as empty.
func namedInCell(cell string) []string {
	if cell == "none" || cell == "" {
		return nil
	}

	var names []string

	for part := range strings.SplitSeq(cell, ",") {
		names = append(names, bare(strings.TrimSpace(part)))
	}

	return names
}

// Rule 5b: the apps table matches the widgets that exist. It reads the widget
// directories rather than widgetCatalog, because the build discovers widgets by
// directory and the catalog is only the presentation copy — a widget deleted
// with its copy left behind would otherwise go unnoticed. It checks the catalog
// for orphans in both directions for the same reason.
func TestAppsTableMatchesTheWidgets(t *testing.T) {
	built := widgetDirectories(t)
	rows := markdownTable(t, "Resource", "Renders")

	documented := map[string]bool{}

	for _, row := range rows {
		documented[strings.TrimPrefix(bare(row[0]), "ui://cetacean/")] = true
	}

	for _, name := range built {
		if !documented[name] {
			t.Errorf("widget %q has no row in the apps table", name)
		}

		if _, ok := widgetCatalog[name]; !ok {
			t.Errorf(
				"widget %q has no widgetCatalog entry, so it would be served with its bare name",
				name,
			)
		}
	}

	for name := range documented {
		if !slices.Contains(built, name) {
			t.Errorf("apps table lists %q, which is not a widget under %s", name, widgetsPath)
		}
	}

	for name := range widgetCatalog {
		if !slices.Contains(built, name) {
			t.Errorf(
				"widgetCatalog describes %q, which is not a widget under %s",
				name,
				widgetsPath,
			)
		}
	}
}

func widgetDirectories(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(widgetsPath)
	if err != nil {
		t.Fatalf("read %s: %v", widgetsPath, err)
	}

	var names []string

	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, filepath.Base(entry.Name()))
		}
	}

	if len(names) == 0 {
		t.Fatalf("no widget directories under %s", widgetsPath)
	}

	return names
}

// Rule 6: the intro's counts. Four numbers in one sentence that no reader of a
// Go change would think to revisit.
func TestCatalogCountsAreCurrent(t *testing.T) {
	srv := catalogServer(t)

	match := countsPattern.FindStringSubmatch(catalogDoc(t))
	if match == nil {
		t.Fatalf("no count sentence found in %s", catalogPath)
	}

	for _, count := range []struct {
		what       string
		documented string
		actual     int
	}{
		{"resources", match[1], len(staticResources) + len(resourceTemplates)},
		{"tools", match[2], len(srv.toolCatalog())},
		{"prompts", match[3], len(promptCatalog())},
		{"widgets", match[4], len(widgetDirectories(t))},
	} {
		if count.documented != strconv.Itoa(count.actual) {
			t.Errorf(
				"the page says %s %s; there are %d",
				count.documented,
				count.what,
				count.actual,
			)
		}
	}
}
