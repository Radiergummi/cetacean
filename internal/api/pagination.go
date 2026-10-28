package api

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/radiergummi/cetacean/internal/api/problem"
)

type PageParams struct {
	Limit    int
	Offset   int
	Sort     string
	Dir      string
	RangeReq bool

	// Explicit records that the query named a limit, which only the CSV
	// rendering reads. An offset does not count: it says where to start, not
	// how much to take, and the default fifty would then cut the file with
	// nothing inside it to say so.
	Explicit bool
}

var (
	errInvalidLimit  = errors.New(`invalid "limit" parameter: must be a positive integer`)
	errInvalidBefore = errors.New(`invalid "before" parameter: must be a history entry ID`)
)

// parsePagination reads a list's page and order. A malformed value is an error
// naming the parameter, not a reason to fall back to a default; a malformed
// Range is ignored instead, as RFC 9110 §14.2 permits.
func parsePagination(r *http.Request) (PageParams, error) {
	p := PageParams{
		Limit:  50,
		Offset: 0,
		Dir:    "asc",
	}

	q := r.URL.Query()
	var hasQueryPagination bool

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return p, errInvalidLimit
		}
		p.Limit = n
		p.Explicit = true
		hasQueryPagination = true
	}

	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return p, errors.New(`invalid "offset" parameter: must be a non-negative integer`)
		}
		p.Offset = n
		hasQueryPagination = true
	}

	switch dir := strings.ToLower(q.Get("dir")); dir {
	case "", "asc", "desc":
		p.Dir = cmp.Or(dir, "asc")
	default:
		return p, errors.New(`invalid "dir" parameter: must be "asc" or "desc"`)
	}

	// Range header is only used when no query pagination params are present.
	if !hasQueryPagination {
		if rangeLimit, rangeOffset, ok := parseItemsRange(r.Header.Get("Range")); ok {
			p.Limit = rangeLimit
			p.Offset = rangeOffset
			p.RangeReq = true
		}
	}

	if p.Limit > 200 {
		p.Limit = 200
	}

	p.Sort = q.Get("sort")

	return p, nil
}

// parseItemsRange parses a Range header with the "items" unit: "items=a-b",
// inclusive and zero-based, or the "items a-b" spelling it once required.
// Anything else, a set of several ranges included, is no range at all.
func parseItemsRange(header string) (limit, offset int, matched bool) {
	// Range units are case-insensitive (RFC 9110 §14.1).
	const unit = "items"
	if len(header) <= len(unit) || !strings.EqualFold(header[:len(unit)], unit) ||
		header[len(unit)] != '=' && header[len(unit)] != ' ' {
		return 0, 0, false
	}
	spec := header[len(unit)+1:]

	if strings.Contains(spec, ",") {
		return 0, 0, false
	}

	start, end, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, false
	}

	startN, err := strconv.Atoi(strings.TrimSpace(start))
	if err != nil {
		return 0, 0, false
	}

	endN, err := strconv.Atoi(strings.TrimSpace(end))
	if err != nil {
		return 0, 0, false
	}

	if startN < 0 || endN < startN {
		return 0, 0, false
	}

	// The span cannot overflow, but adding one to it can; the caller clamps.
	return min(endN-startN, math.MaxInt-1) + 1, startN, true
}

// ifRange evaluates If-Range on a Range request (RFC 9110 §13.1.5) against the
// collection's validator. When it does not hold, the Range is dropped and the
// whole list goes out; the validator is the same either way.
func ifRange(r *http.Request, validator string) *http.Request {
	cond := r.Header.Get("If-Range")
	if cond == "" || r.Header.Get("Range") == "" || ifRangeHolds(r, cond, validator) {
		return r
	}

	r = r.Clone(r.Context())
	r.Header.Del("Range")
	r.Header.Del("If-Range")

	return r
}

// ifRangeHolds is the strong comparison against the tag this answer would
// carry, in either coding; a weak tag never equals one, and a date never
// holds, since no list sends Last-Modified.
func ifRangeHolds(r *http.Request, cond, validator string) bool {
	cond = strings.TrimSpace(cond)
	if cond == codedETag(validator, EncodingIdentity) {
		return true
	}

	coding := resolveEncoding(r)

	return coding != EncodingIdentity && !compressionDisabled(r) &&
		cond == codedETag(validator, coding)
}

// pageOf is the slice the page names, clamped to what there is.
func pageOf[T any](items []T, p PageParams) []T {
	start := min(p.Offset, len(items))
	end := min(start+p.Limit, len(items))

	return items[start:end]
}

// writeCollectionResponse writes a CollectionResponse, choosing its status from
// whether the request used Range: 206 with Content-Range for a partial answer,
// 416 for an offset beyond the total, 200 otherwise. A query-param request gets
// 200 with Link headers, and Accept-Ranges: items is always set.
//
// validator is the tag the caller already offered on the conditional path, or
// empty to hash the body.
func writeCollectionResponse[T any](
	w http.ResponseWriter,
	r *http.Request,
	resp CollectionResponse[T],
	p PageParams,
	validator string,
) {
	w.Header().Set("Accept-Ranges", "items")

	if p.RangeReq {
		if resp.Total == 0 {
			writeCachedJSONStatusValidated(w, r, http.StatusOK, resp, validator)
			return
		}

		if resp.Offset >= resp.Total {
			// The preconditions come before the Range (RFC 9110 §13.2.2).
			if validator != "" {
				if refusedByPrecondition(w, r, validator, time.Time{}) {
					return
				}
				if tagged := matchedValidator(r, validator); tagged != "" {
					writeNotModified(w, tagged)
					return
				}
			}

			w.Header().Set("Content-Range", fmt.Sprintf("items */%d", resp.Total))
			problem.WriteStatus(w, r, http.StatusRequestedRangeNotSatisfiable,
				"range start is beyond the total number of items")
			return
		}

		last := resp.Offset + len(resp.Items) - 1
		if resp.Offset == 0 && last >= resp.Total-1 {
			writeCachedJSONStatusValidated(w, r, http.StatusOK, resp, validator)
			return
		}

		w.Header().Set(
			"Content-Range",
			fmt.Sprintf("items %d-%d/%d", resp.Offset, last, resp.Total),
		)
		writeCachedJSONStatusValidated(w, r, http.StatusPartialContent, resp, validator)
		return
	}

	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeCachedJSONStatusValidated(w, r, http.StatusOK, resp, validator)
}

// writeLinkTemplate sets a Link-Template header (RFC 9652) on the response,
// allowing clients to construct detail URLs from collection items.
// The template should be a relative path with RFC 6570 variables,
// e.g. "/services/{id}".
func writeLinkTemplate(w http.ResponseWriter, r *http.Request, template string) {
	w.Header().Add("Link-Template", fmt.Sprintf(
		`<%s>; rel="item"`,
		absPath(r.Context(), template),
	))
}

func writePaginationLinks(w http.ResponseWriter, r *http.Request, total, limit, offset int) {
	buildLink := func(newOffset int) string {
		q := r.URL.Query()
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(newOffset))
		return fmt.Sprintf("<%s?%s>", selfPath(r), q.Encode())
	}

	// Compared without adding, so an offset near the largest int cannot wrap.
	var links []string
	if offset < total && limit < total-offset {
		links = append(links, buildLink(offset+limit)+`; rel="next"`)
	}
	if offset > 0 {
		prev := max(min(offset, total)-limit, 0)
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
