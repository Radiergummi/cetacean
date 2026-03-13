package api

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

type PageParams struct {
	Limit  int
	Offset int
	Sort   string
	Dir    string
}

func parsePagination(r *http.Request) PageParams {
	p := PageParams{
		Limit:  50,
		Offset: 0,
		Dir:    "asc",
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.Limit = n
		}
	}
	if p.Limit > 200 {
		p.Limit = 200
	}

	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			p.Offset = n
		}
	}

	p.Sort = r.URL.Query().Get("sort")
	if v := r.URL.Query().Get("dir"); v != "" {
		p.Dir = v
	}
	if p.Dir != "desc" {
		p.Dir = "asc"
	}

	return p
}

func applyPagination[T any](items []T, p PageParams) CollectionResponse[T] {
	total := len(items)

	start := min(p.Offset, total)
	end := min(start+p.Limit, total)

	result := items[start:end]
	if result == nil {
		result = []T{}
	}

	return NewCollectionResponse(result, total, p.Limit, p.Offset)
}

func writePaginationLinks(w http.ResponseWriter, r *http.Request, total, limit, offset int) {
	buildLink := func(newOffset int) string {
		q := r.URL.Query()
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(newOffset))
		return fmt.Sprintf("<%s?%s>", r.URL.Path, q.Encode())
	}

	var links []string
	if offset+limit < total {
		links = append(links, buildLink(offset+limit)+`; rel="next"`)
	}
	if offset > 0 {
		prev := max(offset-limit, 0)
		links = append(links, buildLink(prev)+`; rel="prev"`)
	}
	if len(links) > 0 {
		w.Header().Add("Link", strings.Join(links, ", "))
	}
}

func sortItems[T any](items []T, key, dir string, accessors map[string]func(T) string) []T {
	accessor, ok := accessors[key]
	if !ok {
		return items
	}

	// Pre-compute lowered sort keys to avoid repeated strings.ToLower in comparisons.
	keys := make([]string, len(items))
	for i, item := range items {
		keys[i] = strings.ToLower(accessor(item))
	}

	// Build an index slice and sort it.
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}

	desc := strings.EqualFold(dir, "desc")
	slices.SortStableFunc(idx, func(a, b int) int {
		if desc {
			return cmp.Compare(keys[b], keys[a])
		}
		return cmp.Compare(keys[a], keys[b])
	})

	cp := make([]T, len(items))
	for i, j := range idx {
		cp[i] = items[j]
	}
	return cp
}
