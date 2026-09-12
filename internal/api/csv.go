package api

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strings"
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

// csvPage narrows a list to the page the request named. Only a limit
// truncates; an offset names where to start and applies on its own. A Range
// request takes neither: CSV answers 200 with no Content-Range, and a partial
// body under that status would be a lie.
func csvPage[T any](items []T, p PageParams) []T {
	if p.RangeReq {
		return items
	}

	if p.Explicit {
		return pageOf(items, p)
	}

	if p.Offset >= len(items) {
		return nil
	}

	return items[p.Offset:]
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

// csvFilename dates the file so a directory of exports stays apart.
func csvFilename(name string, at time.Time) string {
	return filenamePart(name) + "-" + at.UTC().Format(time.DateOnly) + ".csv"
}

// filenamePart reduces a name to what an RFC 6266 quoted-string holds
// literally, so no filename* leg is needed. A task export carries its parent's
// name, and neither a hostname nor a quote is guaranteed ASCII.
func filenamePart(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, name)
}
