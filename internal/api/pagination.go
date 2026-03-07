package api

import (
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

type PagedResponse[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
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

	return p
}

func applyPagination[T any](items []T, p PageParams) PagedResponse[T] {
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

	return PagedResponse[T]{
		Items: result,
		Total: total,
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
