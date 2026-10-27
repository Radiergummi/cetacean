package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/filter"
)

// listSpec describes a resource list endpoint. The generic helpers handleList
// and prepareList use it to drive the standard filter → search → expr-filter →
// sort → paginate → respond pipeline.
type listSpec[T any] struct {
	resourceType string                                        // for setAllowList / Allow header
	linkTemplate string                                        // for Link-Template header (e.g. "/services/{id}")
	list         func() []T                                    // cache list method
	aclName      func(T) string                                // maps item → its ACL name; resourceType supplies the type
	searchName   func(T) string                                // nil = no search support
	filterEnv    filter.Env[T]                                 // filter.Nodes, filter.Tasks, …
	sortKeys     map[string]func(T) string                     // sort field accessors
	prepare      func([]T) []T                                 // optional pre-filter transform (e.g. strip secret data)
	versions     *versionedSpec[T]                             // optional: groups a config or secret list into versions
	itemType     string                                        // JSON-LD @type for each item (e.g. "Node")
	idFunc       func(T) string                                // extracts JSON-LD @id path for each item
	rows         func([]T) []cluster.Row                       // builds the CSV rendering; required unless csv is set
	csv          func(http.ResponseWriter, *http.Request, []T) // writes a CSV whose columns a Row can't carry
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
// Each item is wrapped with JSON-LD @id and @type fields.
func handleList[T any](h *Handlers, w http.ResponseWriter, r *http.Request, spec listSpec[T]) {
	handleListOf(
		h,
		w,
		r,
		spec,
		spec.idFunc,
		func(page []T, _ cluster.VersionView) []T { return page },
	)
}

// handleListOf is handleList for a list whose page renders into another shape.
// What render adds must come from the cache and the caller's grants, or the
// derived validator no longer covers it.
func handleListOf[T, U any](
	h *Handlers,
	w http.ResponseWriter,
	r *http.Request,
	spec listSpec[T],
	idFunc func(U) string,
	render func(page []T, versions cluster.VersionView) []U,
) {
	// A query the list refuses is refused before any 304: no validator vouches
	// for a request that has no answer.
	var sel cluster.VersionSelection
	if spec.versions != nil {
		var err error
		if sel, err = versionSelection(r); err != nil {
			writeErrorCode(w, r, "API016", err.Error())
			return
		}
	}

	// A list is a pure function of the cache, the caller's grants and the
	// request, so the validator is knowable before any of the work, and a 304
	// skips the cache read, filtering, sort and marshal it would otherwise pay.
	validator := h.derivedETag(r)
	if listNotModified(h, w, r, spec, validator) {
		return
	}

	r, validator = h.ifRange(r, validator)

	items, versions, p, ok := prepareList(h, w, r, spec, sel)
	if !ok {
		return
	}

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		rows := spec.rows
		if spec.versions != nil {
			rows = func(page []T) []cluster.Row {
				out := spec.rows(page)
				cluster.AnnotateVersionRows(out, versions)

				return out
			}
		}

		if spec.csv != nil {
			spec.csv(w, r, csvPage(items, p))
		} else {
			writeListCSV(w, r, spec.resourceType, items, p, rows)
		}

		return
	}

	writeLinkTemplate(w, r, spec.linkTemplate)
	writeListPage(w, r, items, p, validator, spec.itemType, idFunc,
		func(page []T) []U { return render(page, versions) })
}

// writeListPage writes the page of items p names as the JSON collection, each
// entry wrapped with @id and @type; render turns the page into those entries.
func writeListPage[T, U any](
	w http.ResponseWriter,
	r *http.Request,
	items []T,
	p PageParams,
	validator string,
	itemType string,
	idFunc func(U) string,
	render func(page []T) []U,
) {
	writeCollectionResponse(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(r.Context(), render(pageOf(items, p)), itemType, idFunc),
			len(items),
			p.Limit,
			p.Offset,
		),
		p,
		validator,
	)
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
	sel cluster.VersionSelection,
) ([]T, cluster.VersionView, PageParams, bool) {
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

	// Grouped over every readable item, before a search narrows them.
	var versions cluster.VersionView
	if spec.versions != nil {
		versions = spec.versions.view(items, h.readChecker(r))
		items = cluster.SelectVersions(items, spec.versions.id, versions.Groups, sel)
	}

	if spec.searchName != nil {
		items = searchFilter(items, r.URL.Query().Get("search"), spec.searchName)
	}

	var ok bool
	if items, ok = exprFilter(items, r.URL.Query().Get("filter"), spec.filterEnv, w, r); !ok {
		return nil, versions, PageParams{}, false
	}

	p, err := parsePagination(r)
	if err != nil {
		writeProblem(w, r, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return nil, versions, PageParams{}, false
	}

	items = sortItems(items, p.Sort, p.Dir, spec.sortKeys)

	return items, versions, p, true
}
