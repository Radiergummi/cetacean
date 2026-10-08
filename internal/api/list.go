package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// listSpec describes a resource list endpoint. The generic helpers handleList
// and prepareList use it to drive the standard filter → search → expr-filter →
// sort → paginate → respond pipeline.
type listSpec[T any] struct {
	resourceType string                                 // for setAllowList / Allow header
	linkTemplate string                                 // for Link-Template header (e.g. "/services/{id}")
	list         func() []T                             // cache list method
	aclName      func(T) string                         // maps item → its ACL name; resourceType supplies the type
	searchName   func(T) string                         // nil = no search support
	filterEnv    func(T, map[string]any) map[string]any // filter.XxxEnv
	sortKeys     map[string]func(T) string              // sort field accessors
	prepare      func([]T) []T                          // optional pre-filter transform (e.g. strip secret data)
	itemType     string                                 // JSON-LD @type for each item (e.g. "Node")
	idFunc       func(T) string                         // extracts JSON-LD @id path for each item
	rows         func([]T) []cluster.Row                // builds the CSV rendering; required
}

// listNotModified answers a matching conditional request with 304, and reports
// whether it did.
//
// A 304 carries what a client still reads off it — the ETag, the validators'
// Vary, and the Allow header the dashboard gates its controls on, which costs
// nothing to compute. It does not carry the pagination Link headers or
// Content-Range: those need the totals, and producing them is exactly the work
// being skipped. RFC 9110 §15.4.5 asks for neither.
func listNotModified[T any](
	h *Handlers,
	w http.ResponseWriter,
	r *http.Request,
	spec listSpec[T],
	validator string,
) bool {

	tagged := matchedValidator(r, validator)
	if tagged == "" {
		return false
	}

	h.setAllowList(w, r, spec.resourceType)
	writeLinkTemplate(w, r, spec.linkTemplate)
	w.Header().Set("Accept-Ranges", "items")
	writeNotModified(w, tagged)

	return true
}

// handleList runs the full list pipeline and writes the JSON response.
// Use this for resources that need no post-pagination transformation.
// When spec.itemType and spec.idFunc are set, each item is wrapped with
// JSON-LD @id and @type fields.
func handleList[T any](h *Handlers, w http.ResponseWriter, r *http.Request, spec listSpec[T]) {
	// A list is a pure function of the cache, the caller's grants and the
	// request, so the validator is knowable before any of the work. Answering
	// here skips the cache read, the filtering, the sort and the marshal that
	// a 304 would otherwise pay for in full.
	validator := h.derivedETag(r)
	if listNotModified(h, w, r, spec, validator) {
		return
	}

	items, p, ok := prepareList(h, w, r, spec)
	if !ok {
		return
	}

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		writeListCSV(w, r, spec.resourceType, items, p, spec.rows)
		return
	}

	writeLinkTemplate(w, r, spec.linkTemplate)

	if spec.itemType != "" && spec.idFunc != nil {
		raw := applyPagination(r.Context(), items, p)
		wrapped := CollectionResponse[Item[T]]{
			Context: raw.Context,
			Type:    raw.Type,
			Items:   wrapItems(r.Context(), raw.Items, spec.itemType, spec.idFunc),
			Total:   raw.Total,
			Limit:   raw.Limit,
			Offset:  raw.Offset,
		}
		writeCollectionResponse(w, r, wrapped, p, validator)
		return
	}

	resp := applyPagination(r.Context(), items, p)
	writeCollectionResponse(w, r, resp, p, validator)
}

// prepareList runs steps 1–7 of the list pipeline (allow header, cache fetch,
// ACL filter, optional prepare, search, expr filter, pagination parse, sort)
// and returns the sorted items plus pagination params. Returns false if an
// error response was already written.
func prepareList[T any](
	h *Handlers,
	w http.ResponseWriter,
	r *http.Request,
	spec listSpec[T],
) ([]T, PageParams, bool) {
	h.setAllowList(w, r, spec.resourceType)
	items := spec.list()

	if spec.prepare != nil {
		items = spec.prepare(items)
	}

	// In place: items is the copy spec.list() just made, and prepareList is
	// the only thing holding it. Named rather than by resource string, so a
	// filtered list does not build one per item for the matcher to split.
	items = acl.FilterInPlaceNamed(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		items,
		spec.resourceType,
		spec.aclName,
	)

	if spec.searchName != nil {
		items = searchFilter(items, r.URL.Query().Get("search"), spec.searchName)
	}

	var ok bool
	if items, ok = exprFilter(items, r.URL.Query().Get("filter"), spec.filterEnv, w, r); !ok {
		return nil, PageParams{}, false
	}

	p, err := parsePagination(r)
	if err != nil {
		writeProblem(w, r, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return nil, PageParams{}, false
	}

	items = sortItems(items, p.Sort, p.Dir, spec.sortKeys)

	return items, p, true
}
