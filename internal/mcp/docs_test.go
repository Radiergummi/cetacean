package mcp

import (
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/config"
)

// These tests hold docs/mcp-tools.mdx against the catalog it documents. The
// page states an operations level, an argument list and a set of accepted
// values for each of 27 tools, a table of prompts, a table of widgets, and four
// counts — all typed by hand against this package.
//
// They live here, in `package mcp`, because everything they read is unexported:
// a script outside the package would need toolCatalog, toolDef.tier,
// promptCatalog, promptTier and uiResources exported first, which is a new
// public surface on a domain package for the docs site's sake. That also puts
// them in `go test ./...`, which `ci.yml` runs on every pull request including
// a documentation-only one — and over this repository's history the catalog
// files moved in 8 commits where `docs/` moved in 178, so a prose rewrite
// dropping an argument is the likelier drift by far.
//
// Most rules run in both directions: the page must state every fact the catalog
// holds, and must state no fact it does not. The exception is the value rule,
// which reads a whole card and would otherwise flag every tool the prose names
// in passing. See docs/specs/2026-09-09-mcp-catalog-drift-check-design.md.
const catalogPath = "../../docs/mcp-tools.mdx"

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
// its whole body. The last two are kept apart because the rules read them at
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

	cards := map[string]card{}

	for _, match := range cardPattern.FindAllStringSubmatch(catalogDoc(t), -1) {
		name, body := match[1], match[3]

		// The pattern matches a single digit, so this cannot fail.
		level, _ := strconv.Atoi(match[2])

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

// documentedNames reads the arguments fact with parenthesised groups removed,
// which is the page's convention: an argument is named bare, and the values it
// accepts go in parentheses after it.
//
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
// none. mcp-go's Enum option writes a []string; anything else is not an enum
// this reads. Values nested under `items` are out of reach and unchecked.
func schemaEnum(property any) []string {
	fields, ok := property.(map[string]any)
	if !ok {
		return nil
	}

	values, _ := fields["enum"].([]string)

	return values
}

// Rules 1 and 2: the page documents every tool, documents no tool that does not
// exist, and gives each the level that actually gates it.
func TestEveryToolHasACardAtItsTier(t *testing.T) {
	srv := newTestServer(t)
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

// Rule 3: a card's arguments fact names every argument its tool accepts, and
// names nothing else. The second direction is what keeps the first honest. It
// costs no false positives — a fact naming a value bare would be claiming that
// value is an argument — and it catches the reflow that moves a value out of
// its parentheses, which would otherwise quietly widen what counts as named.
func TestEveryToolArgumentIsDocumented(t *testing.T) {
	srv := newTestServer(t)
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

		for name := range named {
			if _, ok := def.tool.InputSchema.Properties[name]; !ok {
				t.Errorf(
					"card %q names %q as an argument, which the tool does not accept "+
						"(the values an argument accepts belong in parentheses)",
					def.tool.Name, name,
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
	srv := newTestServer(t)
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

// markdownTable returns the rows of the first table whose header holds every
// given heading, each row keyed by heading. Keying by heading rather than by
// position is what stops a column being inserted, or two being swapped, from
// shifting every comparison into a failure that reads like drift.
func markdownTable(t *testing.T, headings ...string) []map[string]string {
	t.Helper()

	lines := strings.Split(catalogDoc(t), "\n")

	for index, line := range lines {
		if !isTableRow(line) {
			continue
		}

		header := tableCells(line)
		if !containsAll(header, headings) {
			continue
		}

		// Markdown puts the separator immediately under the header, so the rows
		// start two lines down.
		rows := tableRows(header, lines[min(index+2, len(lines)):])
		if len(rows) == 0 {
			t.Fatalf("the table with headings %v in %s has no rows", headings, catalogPath)
		}

		return rows
	}

	t.Fatalf("no table with headings %v found in %s", headings, catalogPath)

	return nil
}

// tableRows reads rows until the table ends at a blank line. A line that is not
// a row and not blank continues the previous row's last cell: the page is
// hand-wrapped, and its own section table wraps rows this way, so a reflow of
// either table read here would otherwise truncate it — reporting every row past
// the wrap as missing, which reads exactly like drift that is not there.
func tableRows(header, lines []string) []map[string]string {
	var rows []map[string]string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			break
		}

		if !isTableRow(line) {
			if len(rows) > 0 {
				last := header[len(header)-1]
				rows[len(rows)-1][last] = strings.TrimSpace(rows[len(rows)-1][last] + " " + trimmed)
			}

			continue
		}

		row := map[string]string{}

		for column, cell := range tableCells(line) {
			if column < len(header) {
				row[header[column]] = cell
			}
		}

		rows = append(rows, row)
	}

	return rows
}

func isTableRow(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "|")
}

func tableCells(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
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

func sorted(names []string) []string {
	return slices.Sorted(slices.Values(names))
}

// namedInCell reads a comma-separated table cell, treating "none" as empty. An
// empty cell is left as one, so it fails against whatever the catalog declares
// rather than passing as "nothing declared".
func namedInCell(cell string) []string {
	if cell == "none" {
		return nil
	}

	var names []string

	for part := range strings.SplitSeq(cell, ",") {
		names = append(names, bare(strings.TrimSpace(part)))
	}

	return names
}

// Rule 5a: the prompts table's mechanical columns — the tier promptTier derives
// from the tools a prompt drives, its argument, and the resource types it needs
// read access to — match the catalog. Only the last column is prose.
func TestPromptsTableMatchesTheCatalog(t *testing.T) {
	srv := newTestServer(t)
	tiers := srv.toolTiers()
	catalog := promptCatalog()

	documented := map[string]map[string]string{}

	for _, row := range markdownTable(t, "Prompt", "Level", "Argument", "Reads") {
		documented[bare(row["Prompt"])] = row
	}

	for _, def := range catalog {
		row, ok := documented[def.prompt.Name]
		if !ok {
			t.Errorf("prompt %q has no row in the prompts table", def.prompt.Name)

			continue
		}

		if want := strconv.Itoa(int(promptTier(def, tiers))); row["Level"] != want {
			t.Errorf(
				"prompt %q: table says level %s, catalog derives %s",
				def.prompt.Name, row["Level"], want,
			)
		}

		var arguments []string

		for _, argument := range def.prompt.Arguments {
			arguments = append(arguments, argument.Name)
		}

		// Sorted on both sides: neither the cell's comma order nor the slice's
		// carries meaning, so alphabetising either is not drift.
		if got, want := sorted(namedInCell(row["Argument"])), sorted(arguments); !slices.Equal(
			got,
			want,
		) {
			t.Errorf(
				"prompt %q: table lists arguments %v, catalog declares %v",
				def.prompt.Name, got, want,
			)
		}

		if got, want := sorted(namedInCell(row["Reads"])), sorted(def.reads); !slices.Equal(
			got,
			want,
		) {
			t.Errorf(
				"prompt %q: table lists reads %v, catalog declares %v",
				def.prompt.Name, got, want,
			)
		}
	}

	for name := range documented {
		if !slices.ContainsFunc(
			catalog,
			func(def promptDef) bool { return def.prompt.Name == name },
		) {
			t.Errorf("prompts table lists %q, which the catalog does not register", name)
		}
	}
}

// Rule 5b: the apps table matches the widgets that exist, in both directions.
// It reads uiResources rather than the widget source directories, because that
// is the registry the server serves from — a widget whose source is present but
// whose bundle did not emit is not one a host can render, and the page
// documents what a host can reach. ui_test.go's TestMain points that at
// frontend/dist-widgets for the whole package.
func TestAppsTableMatchesTheWidgets(t *testing.T) {
	served := uiResources()
	if len(served) == 0 {
		t.Fatal("no widget resources: did `npm run build:widgets` run before `go test`?")
	}

	documented := map[string]bool{}

	for _, row := range markdownTable(t, "Resource", "Renders") {
		documented[strings.TrimPrefix(bare(row["Resource"]), "ui://cetacean/")] = true
	}

	for _, resource := range served {
		if !documented[resource.Name] {
			t.Errorf("widget %q has no row in the apps table", resource.Name)
		}
	}

	for name := range documented {
		if !slices.ContainsFunc(served, func(r uiResource) bool { return r.Name == name }) {
			t.Errorf("apps table lists %q, which the server does not serve", name)
		}
	}
}

// Rule 6: the intro's counts. Four numbers in one sentence that no reader of a
// Go change would think to revisit.
func TestCatalogCountsAreCurrent(t *testing.T) {
	srv := newTestServer(t)

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
		{"widgets", match[4], len(uiResources())},
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
