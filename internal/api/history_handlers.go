package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// --- History ---

func (h *Handlers) HandleHistory(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	query, ok := h.timelineQuery(w, r)
	if !ok {
		return
	}

	entries := h.timeline(r, query)
	h.linkAnnotationsWhen(w, r, h.mayAnnotate(r))

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		writeCSV(w, r, "history", csvTableForHistory(entries))
		return
	}

	linkNextTimelinePage(w, r, entries, query.Limit)
	writeCachedJSON(w, r, NewCollectionResponse(
		r.Context(), wrapHistory(r.Context(), entries), len(entries), len(entries), 0,
	))
}

// HandleAnnotations lists the annotations, each in the shape its own
// /annotations/{id} has.
func (h *Handlers) HandleAnnotations(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	mayAnnotate := h.mayAnnotate(r)

	offer := readOnly
	if mayAnnotate {
		offer = offeredWrites{methods: []string{"GET", "HEAD", "POST"}}
	}
	writeAllow(w, offer)

	if !h.requireAnnotations(w, r) || refuseParams(w, r, "kind", "tasks") {
		return
	}

	query, ok := h.timelineQuery(w, r)
	if !ok {
		return
	}

	query.Kinds = []string{cache.KindAnnotation}
	entries := h.timeline(r, query)
	h.linkAnnotationsWhen(w, r, mayAnnotate)

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		writeCSV(w, r, "annotations", csvTableForHistory(entries))
		return
	}

	annotations := make([]cache.Annotation, len(entries))
	for i, e := range entries {
		annotations[i] = listedAnnotation(*e.Annotation)
	}

	linkNextTimelinePage(w, r, entries, query.Limit)
	writeCachedJSON(w, r, NewCollectionResponse(
		r.Context(),
		wrapTypedItems(r.Context(), annotations,
			func(cache.Annotation) string { return "Annotation" },
			func(a cache.Annotation) string { return annotationPath(a.ID) },
		),
		len(entries), len(entries), 0,
	))
}

// timelineQuery reads what /history's and /annotations' JSON and CSV share:
// the cursor, and the scope their feeds read too.
func (h *Handlers) timelineQuery(
	w http.ResponseWriter,
	r *http.Request,
) (cache.HistoryQuery, bool) {
	// Paged by cursor, as the feeds are: the ring moves under an offset.
	beforeID, limit, err := parseFeedPagination(r)
	if err != nil {
		problem.Write(w, r, "API016", err.Error())
		return cache.HistoryQuery{}, false
	}

	// A download nobody paginated is everything the ring and the annotations hold.
	if ContentTypeFromContext(r.Context()) == ContentTypeCSV && r.URL.Query().Get("limit") == "" {
		limit = h.cache.History().Size() + h.cache.AnnotationCount()
	}

	query, ok := h.timelineScope(w, r)
	query.BeforeID, query.Limit = beforeID, limit

	return query, ok
}

// timelineScope reads what every representation of the timeline shares: the
// window, the resource, `tasks` and the filters, with the caller's read checks.
func (h *Handlers) timelineScope(
	w http.ResponseWriter,
	r *http.Request,
) (cache.HistoryQuery, bool) {
	q := r.URL.Query()

	window, err := cluster.ParseTimeWindow(q.Get("since"), q.Get("until"))
	if err != nil {
		problem.Write(w, r, "API016", err.Error())
		return cache.HistoryQuery{}, false
	}

	// `resourceId` is a query parameter, so the canonical redirect never
	// reaches it and a caller holding a name would read an empty timeline.
	resourceType := q.Get("type")

	canRead := h.acl.Checker(auth.IdentityFromContext(r.Context()), "read")

	resourceID, err := cluster.ResolveIdentifier(
		h.cache, resourceType, q.Get("resourceId"), canRead,
	)
	if h.reportAmbiguousName(w, r, resourceType, err) {
		return cache.HistoryQuery{}, false
	}

	query := cache.HistoryQuery{
		Type:       cache.EventType(resourceType),
		ResourceID: resourceID,
		After:      window.Since,
		Before:     window.Until,
		Visible:    h.readableHistory(r),

		ReadableAnnotation: h.annotationCaller(r).MayRead,
	}

	if err := timelineFilters(q, &query); err != nil {
		problem.Write(w, r, "API016", err.Error())
		return cache.HistoryQuery{}, false
	}

	includeTasks := true
	if value := q.Get("tasks"); value != "" {
		if includeTasks, err = strconv.ParseBool(value); err != nil {
			problem.Write(w, r, "API016", "tasks must be true or false")
			return cache.HistoryQuery{}, false
		}
	}

	// The type names what resourceId is, and a service's timeline holds its
	// tasks' entries too, unless the caller turns that off.
	if includeTasks && query.Type == cache.EventService && query.ResourceID != "" {
		query.Type = ""
		query.Types = []cache.EventType{cache.EventService, cache.EventTask}
	}

	return query, true
}

// timelineFilters reads the filters the timeline's JSON, CSV and feeds share:
// `search`; `kind`, `tag` and `author`, which repeat; and `matchAny`, which
// widens the tags to any of them. An empty value is no filter.
func timelineFilters(q url.Values, query *cache.HistoryQuery) error {
	present := func(name string) []string {
		return slices.DeleteFunc(slices.Clone(q[name]), func(v string) bool { return v == "" })
	}

	query.Search = q.Get("search")

	query.Kinds = present("kind")
	query.Tags = present("tag")
	query.Authors = present("author")

	for _, kind := range query.Kinds {
		if !slices.Contains(cache.Kinds, kind) {
			return fmt.Errorf("kind %q is not one of %s", kind, strings.Join(cache.Kinds, ", "))
		}
	}

	if value := q.Get("matchAny"); value != "" {
		matchAny, err := strconv.ParseBool(value)
		if err != nil {
			return errors.New("matchAny must be true or false")
		}

		query.MatchAny = matchAny
	}

	return nil
}

// timeline lists query as the caller sees it, through cluster.ReadTimeline.
func (h *Handlers) timeline(r *http.Request, query cache.HistoryQuery) []cache.HistoryEntry {
	canRead := h.acl.Checker(auth.IdentityFromContext(r.Context()), "read")
	entries, present := cluster.ReadTimeline(h.cache, query, canRead)

	return present(entries)
}

// linkNextTimelinePage links the page below the last entry when this one is full.
func linkNextTimelinePage(
	w http.ResponseWriter,
	r *http.Request,
	entries []cache.HistoryEntry,
	limit int,
) {
	if len(entries) < limit || len(entries) == 0 {
		return
	}

	next := r.URL.Query()
	next.Set("before", strconv.FormatUint(entries[len(entries)-1].ID, 10))
	next.Set("limit", strconv.Itoa(limit))
	w.Header().Add("Link", fmt.Sprintf(`<%s?%s>; rel="next"`, selfPath(r), next.Encode()))
}

// wrapHistory types each item by its kind: a gap is not a change.
func wrapHistory(ctx context.Context, entries []cache.HistoryEntry) []Item[cache.HistoryEntry] {
	return wrapTypedItems(
		ctx,
		entries,
		func(e cache.HistoryEntry) string {
			switch e.Kind {
			case cache.KindGap:
				return "Gap"
			case cache.KindAnnotation:
				return "Annotation"
			default:
				return "HistoryEntry"
			}
		},
		func(e cache.HistoryEntry) string {
			if e.Kind == cache.KindAnnotation {
				return annotationPath(e.ID)
			}

			return fmt.Sprintf("/history#%d", e.ID)
		},
	)
}
