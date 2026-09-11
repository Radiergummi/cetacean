package api

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"time"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// csvTable is a rendered table: the header row, then the records under it.
type csvTable struct {
	header  []string
	records [][]string
}

// renderCSV writes a table as RFC 4180 text/csv.
//
// The escaping is the standard library's: it quotes a field carrying a comma,
// a quote or a line break, doubles inner quotes, and — with UseCRLF — ends
// every record with CRLF, which is what §2 asks for. There is nothing to write
// here beyond turning it on, which is why this is a file rather than a package
// beside api/atom and api/graphml.
func renderCSV(table csvTable) []byte {
	var buf bytes.Buffer

	w := csv.NewWriter(&buf)
	w.UseCRLF = true

	// A bytes.Buffer write cannot fail, so neither can these.
	_ = w.Write(table.header)
	_ = w.WriteAll(table.records)

	return buf.Bytes()
}

// writeCSV renders a table and answers with it, named after the resource it
// lists. It goes out through writeRawWithETag like every other pre-rendered
// body here, so conditional requests and content coding work as they do for
// the JSON beside it.
func writeCSV(w http.ResponseWriter, r *http.Request, name string, table csvTable) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8; header=present")
	w.Header().Set(
		"Content-Disposition",
		`attachment; filename="`+csvFilename(name, time.Now())+`"`,
	)

	writeRawWithETag(w, r, renderCSV(table))
}

// writeListCSV answers a list endpoint's CSV: the page the request asked for,
// or every row when it asked for none.
func writeListCSV[T any](
	w http.ResponseWriter,
	r *http.Request,
	resourceType string,
	items []T,
	p PageParams,
	build func([]T) []cluster.Row,
) {
	writeCSV(w, r, csvPluralize(resourceType), csvTableForRows(
		resourceType,
		csvRowsInOrder(csvPage(items, p), build),
	))
}

// csvPage narrows a list to the page the request named. A request that named
// none is a download of the whole list: the default fifty would truncate an
// export with nothing in the file to say so.
func csvPage[T any](items []T, p PageParams) []T {
	if !p.Explicit {
		return items
	}

	start := min(p.Offset, len(items))
	end := min(start+p.Limit, len(items))

	return items[start:end]
}

// csvRowsInOrder builds rows one item at a time, keeping the order the
// endpoint sorted its items into: every RowsFor* builder ends in a name sort
// of its own, which over a whole slice would discard the request's ?sort=.
func csvRowsInOrder[T any](items []T, build func([]T) []cluster.Row) []cluster.Row {
	rows := make([]cluster.Row, 0, len(items))

	for i := range items {
		rows = append(rows, build(items[i:i+1])...)
	}

	return rows
}

// csvPluralize names the file after the collection rather than the record in
// it. Every resource type here pluralizes with an s.
func csvPluralize(resourceType string) string {
	return resourceType + "s"
}

// csvFilename names the file a browser saves. Dating it keeps a directory of
// exports apart; ASCII throughout, so RFC 6266 needs no filename* leg.
func csvFilename(name string, at time.Time) string {
	return name + "-" + at.UTC().Format(time.DateOnly) + ".csv"
}
