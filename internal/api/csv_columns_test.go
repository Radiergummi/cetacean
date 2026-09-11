package api

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// widgetColumnsPath is the MCP table widget's column map — the other place a
// cluster.Row is projected for a reader rather than a program.
const widgetColumnsPath = "../../frontend/src/widgets/table/columns.ts"

var (
	widgetTypeBlock = regexp.MustCompile(`(?ms)^  (\w+): \[(.*?)\],$`)
	widgetColumn    = regexp.MustCompile(`column\("(\w+)",\s*"([^"]+)"\)`)
)

// TestCSVColumnsMatchTheWidget holds csvRowColumns and columns.ts together.
// Both answer "which of a Row's keys does this type show, and what is each
// called"; they are written in different languages and nothing else compares
// them, so a column added to one would silently be missing from the other.
func TestCSVColumnsMatchTheWidget(t *testing.T) {
	source, err := os.ReadFile(widgetColumnsPath)
	if err != nil {
		t.Fatalf("reading the widget column map: %v", err)
	}

	blocks := widgetTypeBlock.FindAllStringSubmatch(string(source), -1)
	if len(blocks) != len(csvRowColumns) {
		t.Fatalf(
			"found %d types in %s, but csvRowColumns has %d",
			len(blocks), widgetColumnsPath, len(csvRowColumns),
		)
	}

	for _, block := range blocks {
		// The widget keys its map by the plural find takes; a Row carries the
		// singular, and so does csvRowColumns.
		resourceType := strings.TrimSuffix(block[1], "s")

		t.Run(resourceType, func(t *testing.T) {
			want := widgetColumn.FindAllStringSubmatch(block[2], -1)
			if len(want) == 0 {
				t.Fatalf("no columns parsed out of the %s block", block[1])
			}

			got := csvRowColumns[resourceType]
			if got == nil {
				t.Fatalf("the widget shows %s, and csvRowColumns has no such type", block[1])
			}

			if len(got) != len(want) {
				t.Fatalf("shows %d columns, the widget shows %d", len(got), len(want))
			}

			for i, column := range got {
				if key := csvColumnKey(column); key != want[i][1] {
					t.Errorf("column %d reads %q, the widget reads %q", i, key, want[i][1])
				}

				if header := strings.ToLower(want[i][2]); column.header != header {
					t.Errorf(
						"column %d is headed %q, the widget heads it %q",
						i,
						column.header,
						header,
					)
				}
			}
		})
	}
}

// csvColumnKey recovers the Row field a column reads by driving it over a row
// whose every field is its own name. A column stores its reader rather than
// its key, and recovering it here beats carrying a field only a test looks at.
func csvColumnKey(column csvColumn) string {
	value := column.value(cluster.Row{
		ID:      "id",
		Name:    "name",
		Stack:   "stack",
		State:   "state",
		Detail:  "detail",
		Desired: 7,
		Running: 9,
	})

	switch value {
	case "7":
		return "desired"
	case "9":
		return "running"
	default:
		return value
	}
}
