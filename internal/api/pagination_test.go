package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParsePagination_Defaults(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	p, err := parsePagination(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Limit != 50 {
		t.Errorf("expected limit 50, got %d", p.Limit)
	}
	if p.Offset != 0 {
		t.Errorf("expected offset 0, got %d", p.Offset)
	}
	if p.Dir != "asc" {
		t.Errorf("expected dir asc, got %s", p.Dir)
	}
	if p.Sort != "" {
		t.Errorf("expected empty sort, got %s", p.Sort)
	}
}

func TestParsePagination_Custom(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?limit=10&offset=20&sort=name&dir=desc", nil)
	p, err := parsePagination(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Limit != 10 {
		t.Errorf("expected limit 10, got %d", p.Limit)
	}
	if p.Offset != 20 {
		t.Errorf("expected offset 20, got %d", p.Offset)
	}
	if p.Sort != "name" {
		t.Errorf("expected sort name, got %s", p.Sort)
	}
	if p.Dir != "desc" {
		t.Errorf("expected dir desc, got %s", p.Dir)
	}
}

func TestParsePagination_MaxLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?limit=9999", nil)
	p, err := parsePagination(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Limit != 200 {
		t.Errorf("expected limit clamped to 200, got %d", p.Limit)
	}
}

func TestApplyPagination(t *testing.T) {
	items := make([]int, 10)
	for i := range items {
		items[i] = i
	}

	p := PageParams{Limit: 3, Offset: 2}
	result := applyPagination(context.Background(), items, p)

	if result.Total != 10 {
		t.Errorf("expected total 10, got %d", result.Total)
	}
	if len(result.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Items))
	}
	if result.Items[0] != 2 || result.Items[1] != 3 || result.Items[2] != 4 {
		t.Errorf("expected items [2,3,4], got %v", result.Items)
	}
	if result.Context != jsonLDContext {
		t.Errorf("expected @context %s, got %s", jsonLDContext, result.Context)
	}
	if result.Type != "Collection" {
		t.Errorf("expected @type Collection, got %s", result.Type)
	}
	if result.Limit != 3 {
		t.Errorf("expected limit 3, got %d", result.Limit)
	}
	if result.Offset != 2 {
		t.Errorf("expected offset 2, got %d", result.Offset)
	}
}

func TestApplyPagination_BeyondEnd(t *testing.T) {
	items := []int{1, 2, 3}

	p := PageParams{Limit: 10, Offset: 100}
	result := applyPagination(context.Background(), items, p)

	if result.Total != 3 {
		t.Errorf("expected total 3, got %d", result.Total)
	}
	if result.Items == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(result.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(result.Items))
	}
}

type testItem struct {
	Name string
}

func TestWritePaginationLinks_FirstPage(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	writePaginationLinks(w, r, 100, 10, 0)

	link := w.Header().Get("Link")
	if link == "" {
		t.Fatal("expected Link header")
	}
	if !strings.Contains(link, `rel="next"`) {
		t.Error("expected next link")
	}
	if strings.Contains(link, `rel="prev"`) {
		t.Error("first page should not have prev link")
	}
	if !strings.Contains(link, "offset=10") {
		t.Errorf("expected next offset=10, got %s", link)
	}
}

func TestWritePaginationLinks_MiddlePage(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	writePaginationLinks(w, r, 100, 10, 20)

	link := w.Header().Get("Link")
	if !strings.Contains(link, `rel="next"`) {
		t.Error("expected next link")
	}
	if !strings.Contains(link, `rel="prev"`) {
		t.Error("expected prev link")
	}
	if !strings.Contains(link, "offset=30") {
		t.Errorf("expected next offset=30, got %s", link)
	}
	if !strings.Contains(link, "offset=10") {
		t.Errorf("expected prev offset=10, got %s", link)
	}
}

func TestWritePaginationLinks_LastPage(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	writePaginationLinks(w, r, 25, 10, 20)

	link := w.Header().Get("Link")
	if strings.Contains(link, `rel="next"`) {
		t.Error("last page should not have next link")
	}
	if !strings.Contains(link, `rel="prev"`) {
		t.Error("expected prev link")
	}
}

func TestWritePaginationLinks_SinglePage(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	writePaginationLinks(w, r, 5, 10, 0)

	link := w.Header().Get("Link")
	if link != "" {
		t.Errorf("single page should have no Link header, got %s", link)
	}
}

func TestWritePaginationLinks_PrevClampsToZero(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	writePaginationLinks(w, r, 100, 10, 5)

	link := w.Header().Get("Link")
	if !strings.Contains(link, "offset=0") {
		t.Errorf("prev offset should clamp to 0, got %s", link)
	}
}

func TestWritePaginationLinks_PreservesQueryParams(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes?search=worker&sort=name", nil)
	writePaginationLinks(w, r, 100, 10, 0)

	link := w.Header().Get("Link")
	if !strings.Contains(link, "search=worker") {
		t.Errorf("expected search param preserved, got %s", link)
	}
	if !strings.Contains(link, "sort=name") {
		t.Errorf("expected sort param preserved, got %s", link)
	}
}

func TestSortItems(t *testing.T) {
	items := []testItem{{Name: "Charlie"}, {Name: "Alice"}, {Name: "Bob"}}
	accessors := map[string]func(testItem) string{
		"name": func(i testItem) string { return i.Name },
	}

	sorted := sortItems(items, "name", "asc", accessors)

	if sorted[0].Name != "Alice" || sorted[1].Name != "Bob" || sorted[2].Name != "Charlie" {
		t.Errorf(
			"expected [Alice, Bob, Charlie], got [%s, %s, %s]",
			sorted[0].Name,
			sorted[1].Name,
			sorted[2].Name,
		)
	}
	// Original should be unchanged
	if items[0].Name != "Charlie" {
		t.Errorf("original slice was modified")
	}
}

func TestSortItems_Desc(t *testing.T) {
	items := []testItem{{Name: "Alice"}, {Name: "Charlie"}, {Name: "Bob"}}
	accessors := map[string]func(testItem) string{
		"name": func(i testItem) string { return i.Name },
	}

	sorted := sortItems(items, "name", "desc", accessors)

	if sorted[0].Name != "Charlie" || sorted[1].Name != "Bob" || sorted[2].Name != "Alice" {
		t.Errorf(
			"expected [Charlie, Bob, Alice], got [%s, %s, %s]",
			sorted[0].Name,
			sorted[1].Name,
			sorted[2].Name,
		)
	}
}

func TestSortItems_InvalidKey(t *testing.T) {
	items := []testItem{{Name: "Charlie"}, {Name: "Alice"}, {Name: "Bob"}}
	accessors := map[string]func(testItem) string{
		"name": func(i testItem) string { return i.Name },
	}

	sorted := sortItems(items, "unknown", "asc", accessors)

	if sorted[0].Name != "Charlie" || sorted[1].Name != "Alice" || sorted[2].Name != "Bob" {
		t.Errorf(
			"expected original order preserved, got [%s, %s, %s]",
			sorted[0].Name,
			sorted[1].Name,
			sorted[2].Name,
		)
	}
}

func TestParsePagination_RangeBasic(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "items 0-24")

	p, err := parsePagination(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Offset != 0 {
		t.Errorf("expected offset 0, got %d", p.Offset)
	}
	if p.Limit != 25 {
		t.Errorf("expected limit 25, got %d", p.Limit)
	}
	if !p.RangeReq {
		t.Error("expected RangeReq to be true")
	}
}

func TestParsePagination_RangeMaxClamp(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "items 0-999")

	p, err := parsePagination(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Limit != 200 {
		t.Errorf("expected limit clamped to 200, got %d", p.Limit)
	}
	if p.Offset != 0 {
		t.Errorf("expected offset 0, got %d", p.Offset)
	}
	if !p.RangeReq {
		t.Error("expected RangeReq to be true")
	}
}

func TestParsePagination_QueryParamsOverrideRange(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?limit=5&offset=10", nil)
	r.Header.Set("Range", "items 0-24")

	p, err := parsePagination(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Limit != 5 {
		t.Errorf("expected limit 5 from query param, got %d", p.Limit)
	}
	if p.Offset != 10 {
		t.Errorf("expected offset 10 from query param, got %d", p.Offset)
	}
	if p.RangeReq {
		t.Error("expected RangeReq to be false when query params present")
	}
}

func TestParsePagination_RangeNonItemsUnitIgnored(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "bytes 0-1023")

	p, err := parsePagination(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Limit != 50 {
		t.Errorf("expected default limit 50, got %d", p.Limit)
	}
	if p.Offset != 0 {
		t.Errorf("expected default offset 0, got %d", p.Offset)
	}
	if p.RangeReq {
		t.Error("expected RangeReq to be false for non-items unit")
	}
}

func TestParsePagination_RangeMultipartError(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "items 0-9, 50-59")

	_, err := parsePagination(r)
	if err == nil {
		t.Fatal("expected error for multipart range")
	}
	if !errors.Is(err, errMultipartRange) {
		t.Errorf("expected errMultipartRange, got %v", err)
	}
}

func TestParsePagination_InvalidQueryParamsFallBackToRange(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?limit=abc", nil)
	r.Header.Set("Range", "items 0-24")

	p, err := parsePagination(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !p.RangeReq {
		t.Error("expected RangeReq true when query param is invalid")
	}
	if p.Limit != 25 {
		t.Errorf("expected limit 25 from Range header, got %d", p.Limit)
	}
}

func TestWriteCollectionResponse_RangePartial(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nodes", nil)
	r.Header.Set("Range", "items 0-9")
	w := httptest.NewRecorder()

	p := PageParams{Limit: 10, Offset: 0, RangeReq: true}
	resp := CollectionResponse[int]{
		Context: jsonLDContext,
		Type:    "Collection",
		Items:   []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		Total:   100,
		Limit:   10,
		Offset:  0,
	}

	writeCollectionResponse(w, r, resp, p)

	if w.Code != http.StatusPartialContent {
		t.Errorf("expected 206, got %d", w.Code)
	}

	cr := w.Header().Get("Content-Range")
	if cr != "items 0-9/100" {
		t.Errorf("expected Content-Range items 0-9/100, got %q", cr)
	}

	ar := w.Header().Get("Accept-Ranges")
	if ar != "items" {
		t.Errorf("expected Accept-Ranges: items, got %q", ar)
	}
}

func TestWriteCollectionResponse_RangeFullCollection(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nodes", nil)
	r.Header.Set("Range", "items 0-4")
	w := httptest.NewRecorder()

	p := PageParams{Limit: 10, Offset: 0, RangeReq: true}
	resp := CollectionResponse[int]{
		Context: jsonLDContext,
		Type:    "Collection",
		Items:   []int{0, 1, 2, 3, 4},
		Total:   5,
		Limit:   10,
		Offset:  0,
	}

	writeCollectionResponse(w, r, resp, p)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for full collection, got %d", w.Code)
	}

	if cr := w.Header().Get("Content-Range"); cr != "" {
		t.Errorf("expected no Content-Range for full collection, got %q", cr)
	}

	ar := w.Header().Get("Accept-Ranges")
	if ar != "items" {
		t.Errorf("expected Accept-Ranges: items, got %q", ar)
	}
}

func TestWriteCollectionResponse_RangeBeyondTotal(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nodes", nil)
	r.Header.Set("Range", "items 50-59")
	w := httptest.NewRecorder()

	p := PageParams{Limit: 10, Offset: 50, RangeReq: true}
	resp := CollectionResponse[int]{
		Context: jsonLDContext,
		Type:    "Collection",
		Items:   []int{},
		Total:   5,
		Limit:   10,
		Offset:  50,
	}

	writeCollectionResponse(w, r, resp, p)

	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("expected 416, got %d", w.Code)
	}

	cr := w.Header().Get("Content-Range")
	if cr != "items */5" {
		t.Errorf("expected Content-Range items */5, got %q", cr)
	}
}

func TestWriteCollectionResponse_RangeEmptyCollection(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nodes", nil)
	r.Header.Set("Range", "items 0-9")
	w := httptest.NewRecorder()

	p := PageParams{Limit: 10, Offset: 0, RangeReq: true}
	resp := CollectionResponse[int]{
		Context: jsonLDContext,
		Type:    "Collection",
		Items:   []int{},
		Total:   0,
		Limit:   10,
		Offset:  0,
	}

	writeCollectionResponse(w, r, resp, p)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for empty collection, got %d", w.Code)
	}

	ar := w.Header().Get("Accept-Ranges")
	if ar != "items" {
		t.Errorf("expected Accept-Ranges: items, got %q", ar)
	}
}

func TestWriteCollectionResponse_QueryParams(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nodes", nil)
	w := httptest.NewRecorder()

	p := PageParams{Limit: 10, Offset: 0, RangeReq: false}
	resp := CollectionResponse[int]{
		Context: jsonLDContext,
		Type:    "Collection",
		Items:   []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		Total:   100,
		Limit:   10,
		Offset:  0,
	}

	writeCollectionResponse(w, r, resp, p)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	ar := w.Header().Get("Accept-Ranges")
	if ar != "items" {
		t.Errorf("expected Accept-Ranges: items, got %q", ar)
	}

	link := w.Header().Get("Link")
	if !strings.Contains(link, `rel="next"`) {
		t.Errorf("expected next Link header, got %q", link)
	}
}

func TestRangeRequest_EndToEnd(t *testing.T) {
	items := make([]int, 100)
	for i := range items {
		items[i] = i
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := parsePagination(r)
		if err != nil {
			writeProblem(w, r, http.StatusRequestedRangeNotSatisfiable, err.Error())
			return
		}

		resp := applyPagination(r.Context(), items, p)
		writeCollectionResponse(w, r, resp, p)
	})

	t.Run("range returns 206 with Content-Range", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/items", nil)
		r.Header.Set("Range", "items 0-9")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		if w.Code != http.StatusPartialContent {
			t.Errorf("expected 206, got %d", w.Code)
		}

		cr := w.Header().Get("Content-Range")
		if cr != "items 0-9/100" {
			t.Errorf("expected Content-Range items 0-9/100, got %q", cr)
		}

		ar := w.Header().Get("Accept-Ranges")
		if ar != "items" {
			t.Errorf("expected Accept-Ranges: items, got %q", ar)
		}
	})

	t.Run("query params override range", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/items?limit=5&offset=10", nil)
		r.Header.Set("Range", "items 0-9")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		cr := w.Header().Get("Content-Range")
		if cr != "" {
			t.Errorf("expected no Content-Range header, got %q", cr)
		}
	})

	t.Run("multipart range returns 416", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/items", nil)
		r.Header.Set("Range", "items 0-9, 20-29")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		if w.Code != http.StatusRequestedRangeNotSatisfiable {
			t.Errorf("expected 416, got %d", w.Code)
		}
	})

	t.Run("range beyond total returns 416", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/items", nil)
		r.Header.Set("Range", "items 200-209")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		if w.Code != http.StatusRequestedRangeNotSatisfiable {
			t.Errorf("expected 416, got %d", w.Code)
		}

		cr := w.Header().Get("Content-Range")
		if cr != "items */100" {
			t.Errorf("expected Content-Range items */100, got %q", cr)
		}
	})

	t.Run("no range and no query params returns 200 with defaults", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/items", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		ar := w.Header().Get("Accept-Ranges")
		if ar != "items" {
			t.Errorf("expected Accept-Ranges: items, got %q", ar)
		}
	})
}
