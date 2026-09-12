package api

import (
	"strconv"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// csvColumn is one column: the cluster.Row key it reads, and what to head it.
// The header is given because one key means different things per type — detail
// is a service's image, a node's role, a task's node, a network's driver.
type csvColumn struct {
	key    string
	header string
}

// value reads this column off a row. A count renders its zero, unlike the
// JSON: a service scaled to nothing has zero replicas, and that is a fact.
func (c csvColumn) value(row cluster.Row) string {
	switch c.key {
	case "name":
		return row.Name
	case "stack":
		return row.Stack
	case "state":
		return row.State
	case "detail":
		return row.Detail
	case "id":
		return row.ID
	case "desired":
		return strconv.Itoa(row.Desired)
	case "running":
		return strconv.Itoa(row.Running)
	default:
		return ""
	}
}

// csvRowColumns is what each type shows, keyed by the singular name a Row
// carries. TestCSVColumnsMatchTheWidget holds it to the widget's copy.
var csvRowColumns = map[string][]csvColumn{
	"service": {
		{"name", "name"},
		{"stack", "stack"},
		{"state", "state"},
		{"detail", "image"},
		{"desired", "desired"},
		{"running", "running"},
		{"id", "id"},
	},
	"node":    {{"name", "name"}, {"state", "state"}, {"detail", "role"}, {"id", "id"}},
	"task":    {{"name", "name"}, {"state", "state"}, {"detail", "node"}, {"id", "id"}},
	"stack":   {{"name", "name"}, {"desired", "services"}, {"id", "id"}},
	"config":  {{"name", "name"}, {"stack", "stack"}, {"id", "id"}},
	"secret":  {{"name", "name"}, {"stack", "stack"}, {"id", "id"}},
	"network": {{"name", "name"}, {"stack", "stack"}, {"detail", "driver"}, {"id", "id"}},
	"volume":  {{"name", "name"}, {"stack", "stack"}, {"detail", "driver"}, {"id", "id"}},
}

// csvFallbackColumns answers for a type the map has not been taught about: a
// Row carries these four whatever it describes.
var csvFallbackColumns = []csvColumn{
	{"name", "name"},
	{"state", "state"},
	{"detail", "detail"},
	{"id", "id"},
}

func csvColumnsFor(resourceType string) []csvColumn {
	if columns, ok := csvRowColumns[resourceType]; ok {
		return columns
	}

	return csvFallbackColumns
}

// csvTableForHistory renders the change feed, which is not a Row: an entry is
// an event, and what a reader wants from it is when.
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

func csvTableForRecommendations(results []recommendations.Recommendation) csvTable {
	table := csvTable{
		header: []string{
			"severity", "category", "scope", "target", "resource",
			"message", "current", "configured", "suggested",
		},
		records: make([][]string, 0, len(results)),
	}

	for _, result := range results {
		table.records = append(table.records, []string{
			string(result.Severity),
			string(result.Category),
			string(result.Scope),
			result.TargetName,
			result.Resource,
			result.Message,
			csvNumber(result.Current),
			csvNumber(result.Configured),
			csvNumber(result.Suggested),
		})
	}

	return table
}

// csvNumber leaves an absent measurement blank and writes a present one, zero
// included: a service using no CPU measured zero, and blanking that reads as
// though nothing was measured.
func csvNumber(value *float64) string {
	if value == nil {
		return ""
	}

	return strconv.FormatFloat(*value, 'f', -1, 64)
}

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
