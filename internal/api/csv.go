package api

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"time"

	"github.com/radiergummi/cetacean/internal/cluster"
)

type csvTable struct {
	header  []string
	records [][]string
}

// renderCSV writes a table as RFC 4180 text/csv. UseCRLF is what §2 asks for;
// the quoting is the standard library's.
func renderCSV(table csvTable) []byte {
	var buf bytes.Buffer

	w := csv.NewWriter(&buf)
	w.UseCRLF = true

	// A bytes.Buffer write cannot fail.
	_ = w.Write(table.header)
	_ = w.WriteAll(table.records)

	return buf.Bytes()
}

func writeCSV(w http.ResponseWriter, r *http.Request, name string, table csvTable) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8; header=present")
	w.Header().Set(
		"Content-Disposition",
		`attachment; filename="`+csvFilename(name, time.Now())+`"`,
	)

	writeRawWithETag(w, r, renderCSV(table))
}

func writeListCSV[T any](
	w http.ResponseWriter,
	r *http.Request,
	resourceType string,
	items []T,
	p PageParams,
	build func([]T) []cluster.Row,
) {
	// Every type here pluralizes with an s.
	writeCSV(w, r, resourceType+"s", csvTableForRows(
		resourceType,
		csvRowsInOrder(csvPage(items, p), build),
	))
}

// csvPage narrows a list to the page the request named. A request that named
// none gets everything: a file truncated at the default fifty carries nothing
// inside it to say so.
func csvPage[T any](items []T, p PageParams) []T {
	if !p.Explicit {
		return items
	}

	return pageOf(items, p)
}

// csvRowsInOrder builds rows one item at a time, keeping the order the
// endpoint sorted its items into: every RowsFor* builder sorts by name, which
// over a whole slice would discard the request's own sort.
func csvRowsInOrder[T any](items []T, build func([]T) []cluster.Row) []cluster.Row {
	rows := make([]cluster.Row, 0, len(items))

	for i := range items {
		rows = append(rows, build(items[i:i+1])...)
	}

	return rows
}

// csvFilename dates the file so a directory of exports stays apart. ASCII
// throughout, so RFC 6266 needs no filename* leg.
func csvFilename(name string, at time.Time) string {
	return name + "-" + at.UTC().Format(time.DateOnly) + ".csv"
}
