package api

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const widgetColumnsPath = "../../frontend/src/widgets/table/columns.ts"

var (
	widgetTypeBlock = regexp.MustCompile(`(?ms)^  (\w+): \[(.*?)\],$`)
	widgetColumn    = regexp.MustCompile(`column\("(\w+)",\s*"([^"]+)"\)`)
)

// TestCSVColumnsMatchTheWidget holds csvRowColumns and the widget's map
// together. They answer the same question in two languages, and nothing else
// compares them.
func TestCSVColumnsMatchTheWidget(t *testing.T) {
	source, err := os.ReadFile(widgetColumnsPath)
	if err != nil {
		t.Fatalf("reading the widget column map: %v", err)
	}

	blocks := widgetTypeBlock.FindAllStringSubmatch(string(source), -1)

	// A reflow of the widget map lands here rather than reading as a column
	// mismatch.
	if len(blocks) == 0 {
		t.Fatalf("parsed no type blocks out of %s — has it been reformatted?", widgetColumnsPath)
	}

	if len(blocks) != len(csvRowColumns) {
		t.Fatalf(
			"found %d types in %s, but csvRowColumns has %d",
			len(blocks), widgetColumnsPath, len(csvRowColumns),
		)
	}

	for _, block := range blocks {
		// The widget keys its map by the plural; a Row carries the singular.
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
				if column.key != want[i][1] {
					t.Errorf("column %d reads %q, the widget reads %q", i, column.key, want[i][1])
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
