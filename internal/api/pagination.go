package api

import (
	"fmt"
	"net/http"
	"sort"
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

func writePaginationLinks(w http.ResponseWriter, path string, total, limit, offset int) {
	var links []string
	if offset+limit < total {
		links = append(links, fmt.Sprintf("<%s?limit=%d&offset=%d>; rel=\"next\"", path, limit, offset+limit))
	}
	if offset > 0 {
		prev := offset - limit
		if prev < 0 {
			prev = 0
		}
		links = append(links, fmt.Sprintf("<%s?limit=%d&offset=%d>; rel=\"prev\"", path, limit, prev))
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
	sort.SliceStable(cp, func(i, j int) bool {
		a := strings.ToLower(accessor(cp[i]))
		b := strings.ToLower(accessor(cp[j]))
		if desc {
			return a > b
		}
		return a < b
	})

	return cp
}
