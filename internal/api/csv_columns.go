package api

import (
	"strconv"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// csvColumn is one column of a row-based CSV: the header it carries, and what
// it reads off a cluster.Row.
type csvColumn struct {
	header string
	value  func(cluster.Row) string
}

func csvText(header string, read func(cluster.Row) string) csvColumn {
	return csvColumn{header: header, value: read}
}

// csvCount renders an integer column. Zero is a number here rather than the
// blank the JSON omitempty leaves: a service scaled to nothing has zero
// replicas, and a spreadsheet should say so.
func csvCount(header string, read func(cluster.Row) int) csvColumn {
	return csvColumn{
		header: header,
		value:  func(row cluster.Row) string { return strconv.Itoa(read(row)) },
	}
}

var (
	csvName    = csvText("name", func(row cluster.Row) string { return row.Name })
	csvStack   = csvText("stack", func(row cluster.Row) string { return row.Stack })
	csvState   = csvText("state", func(row cluster.Row) string { return row.State })
	csvID      = csvText("id", func(row cluster.Row) string { return row.ID })
	csvRunning = csvCount("running", func(row cluster.Row) int { return row.Running })
)

// csvDetail and csvDesired take their header, because the same Row key means
// something different per type: detail is a service's image, a node's role, a
// task's node and a network's driver, and a stack's desired is how many
// services it holds rather than a replica target.
func csvDetail(header string) csvColumn {
	return csvText(header, func(row cluster.Row) string { return row.Detail })
}

func csvDesired(header string) csvColumn {
	return csvCount(header, func(row cluster.Row) int { return row.Desired })
}

// csvRowColumns is what each resource type shows, keyed by the singular type
// name a Row carries. It is the Go half of frontend/src/widgets/table/
// columns.ts — both project the same cluster.Row for a reader rather than a
// program, and TestCSVColumnsMatchTheWidget holds the two together.
var csvRowColumns = map[string][]csvColumn{
	"service": {
		csvName,
		csvStack,
		csvState,
		csvDetail("image"),
		csvDesired("desired"),
		csvRunning,
		csvID,
	},
	"node":    {csvName, csvState, csvDetail("role"), csvID},
	"task":    {csvName, csvState, csvDetail("node"), csvID},
	"stack":   {csvName, csvDesired("services"), csvID},
	"config":  {csvName, csvStack, csvID},
	"secret":  {csvName, csvStack, csvID},
	"network": {csvName, csvStack, csvDetail("driver"), csvID},
	"volume":  {csvName, csvStack, csvDetail("driver"), csvID},
}

// csvFallbackColumns answers for a type the map has not been taught about: a
// Row carries these four whatever it describes.
var csvFallbackColumns = []csvColumn{csvName, csvState, csvDetail("detail"), csvID}

func csvColumnsFor(resourceType string) []csvColumn {
	if columns, ok := csvRowColumns[resourceType]; ok {
		return columns
	}

	return csvFallbackColumns
}

// csvTableForHistory renders the change feed, which is not a Row: an entry is
// an event rather than a resource, and what a reader wants from it is when.
func csvTableForHistory(entries []cache.HistoryEntry) csvTable {
	table := csvTable{
		header:  []string{"timestamp", "type", "action", "name", "id", "summary"},
		records: make([][]string, 0, len(entries)),
	}

	for _, entry := range entries {
		table.records = append(table.records, []string{
			entry.Timestamp.UTC().Format(time.RFC3339),
			string(entry.Type),
			entry.Action,
			entry.Name,
			entry.ResourceID,
			entry.Summary,
		})
	}

	return table
}

// csvTableForRecommendations renders the findings, which are not Rows either.
func csvTableForRecommendations(results []recommendations.Recommendation) csvTable {
	table := csvTable{
		header: []string{
			"severity", "category", "scope", "target", "resource",
			"message", "current", "configured", "suggested",
		},
		records: make([][]string, 0, len(results)),
	}

	for _, result := range results {
		suggested := ""
		if result.Suggested != nil {
			suggested = csvNumber(*result.Suggested)
		}

		table.records = append(table.records, []string{
			string(result.Severity),
			string(result.Category),
			string(result.Scope),
			result.TargetName,
			result.Resource,
			result.Message,
			csvNumber(result.Current),
			csvNumber(result.Configured),
			suggested,
		})
	}

	return table
}

// csvNumber renders a measurement, leaving an absent one blank: the JSON omits
// a zero here, and a column of zeroes would read as measured.
func csvNumber(value float64) string {
	if value == 0 {
		return ""
	}

	return strconv.FormatFloat(value, 'f', -1, 64)
}

// csvTableForRows projects rows through the columns their type shows.
func csvTableForRows(resourceType string, rows []cluster.Row) csvTable {
	columns := csvColumnsFor(resourceType)

	table := csvTable{
		header:  make([]string, len(columns)),
		records: make([][]string, 0, len(rows)),
	}

	for i, column := range columns {
		table.header[i] = column.header
	}

	for _, row := range rows {
		record := make([]string, len(columns))
		for i, column := range columns {
			record[i] = column.value(row)
		}

		table.records = append(table.records, record)
	}

	return table
}
