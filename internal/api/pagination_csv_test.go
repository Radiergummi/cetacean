package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A CSV download truncates only when the caller named a limit. offset alone
// selects a starting point, not a page size, and the default fifty would cut
// the file with nothing inside it to say so.
func TestCSVOffsetAloneDoesNotTruncate(t *testing.T) {
	items := make([]int, 120)
	for i := range items {
		items[i] = i
	}

	for _, tt := range []struct {
		query string
		want  int
	}{
		{"", 120},
		{"?offset=0", 120},
		{"?offset=100", 20},
		{"?limit=10", 10},
		{"?limit=10&offset=100", 10},
	} {
		t.Run(tt.query, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/services"+tt.query, nil)

			p, err := parsePagination(req)
			if err != nil {
				t.Fatalf("parsePagination: %v", err)
			}

			if got := len(csvPage(items, p)); got != tt.want {
				t.Errorf("csvPage returned %d rows, want %d", got, tt.want)
			}
		})
	}
}

// A Range request still gets every row: CSV answers 200 with no Content-Range,
// and a partial body under that status would be a lie.
func TestCSVRangeRequestStillGetsEveryRow(t *testing.T) {
	items := make([]int, 120)

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	req.Header.Set("Range", "items=0-9")

	p, err := parsePagination(req)
	if err != nil {
		t.Fatalf("parsePagination: %v", err)
	}

	if got := len(csvPage(items, p)); got != 120 {
		t.Errorf("csvPage returned %d rows, want all 120", got)
	}
}
