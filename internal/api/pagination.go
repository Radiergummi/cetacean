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

	start := p.Offset
	if start > total {
		start = total
	}
	end := start + p.Limit
	if end > total {
		end = total
	}

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
		prev := offset - limit
		if prev < 0 {
			prev = 0
		}
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

	cp := make([]T, len(items))
	copy(cp, items)

	desc := strings.EqualFold(dir, "desc")
	slices.SortStableFunc(cp, func(a, b T) int {
		av := strings.ToLower(accessor(a))
		bv := strings.ToLower(accessor(b))
		if desc {
			return cmp.Compare(bv, av)
		}
		return cmp.Compare(av, bv)
	})

	return cp
}
