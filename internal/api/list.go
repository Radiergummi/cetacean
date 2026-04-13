package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
)

// listSpec describes a resource list endpoint. The generic helpers handleList
// and prepareList use it to drive the standard filter → search → expr-filter →
// sort → paginate → respond pipeline.
type listSpec[T any] struct {
	resourceType string                                 // for setAllowList / Allow header
	linkTemplate string                                 // for Link-Template header (e.g. "/services/{id}")
	list         func() []T                             // cache list method
	aclResource  func(T) string                         // maps item → "type:name" for ACL
	searchName   func(T) string                         // nil = no search support
	filterEnv    func(T, map[string]any) map[string]any // filter.XxxEnv
	sortKeys     map[string]func(T) string              // sort field accessors
	prepare      func([]T) []T                          // optional pre-filter transform (e.g. strip secret data)
	itemType     string                                 // JSON-LD @type for each item (e.g. "Node")
	idFunc       func(T) string                         // extracts JSON-LD @id path for each item
}

// handleList runs the full list pipeline and writes the JSON response.
// Use this for resources that need no post-pagination transformation.
// When spec.itemType and spec.idFunc are set, each item is wrapped with
// JSON-LD @id and @type fields.
func handleList[T any](h *Handlers, w http.ResponseWriter, r *http.Request, spec listSpec[T]) {
	items, p, ok := prepareList(h, w, r, spec)
	if !ok {
		return
	}

	writeLinkTemplate(w, r, spec.linkTemplate)

	if spec.itemType != "" && spec.idFunc != nil {
		raw := applyPagination(r.Context(), items, p)
		wrapped := CollectionResponse[Item[T]]{
			Context: raw.Context,
			Type:    raw.Type,
			Items:   wrapItems(raw.Items, spec.itemType, spec.idFunc),
			Total:   raw.Total,
			Limit:   raw.Limit,
			Offset:  raw.Offset,
		}
		writeCollectionResponse(w, r, wrapped, p)
		return
	}

	resp := applyPagination(r.Context(), items, p)
	writeCollectionResponse(w, r, resp, p)
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

	items = acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		items,
		spec.aclResource,
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
