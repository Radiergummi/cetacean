package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// feedHandlers groups the optional feed format handlers for an endpoint.
type feedHandlers struct {
	atom     http.HandlerFunc
	jsonFeed http.HandlerFunc

	// csv marks an endpoint whose JSON handler also renders text/csv. A flag
	// rather than a handler, because the CSV comes off the list that handler
	// has already prepared.
	csv bool

	// csvParams names what the CSV reads, for the alternate link. Empty on a
	// sub-collection that takes none.
	csvParams []string

	// yaml marks an endpoint that also renders a compose document. A handler
	// rather than a flag, because it is a different projection of the
	// resource, not the same body in another encoding.
	yaml http.HandlerFunc

	queryParams []string
}

// servedTypes names what an endpoint carrying these feeds serves, derived from
// the same handlers the switch dispatches on rather than restated beside it.
func (f feedHandlers) servedTypes(sse bool) string {
	types := []string{"application/json", "text/html"}

	if sse {
		types = append(types, "text/event-stream")
	}

	if f.atom != nil {
		types = append(types, "application/atom+xml")
	}

	if f.jsonFeed != nil {
		types = append(types, "application/feed+json")
	}

	if f.csv {
		types = append(types, "text/csv")
	}

	if f.yaml != nil {
		types = append(types, "application/yaml")
	}

	return strings.Join(types, ", ")
}

// contentNegotiated wraps a JSON handler to dispatch based on content type.
// HTML requests go to the SPA, SSE gets 406 (not supported here).
func contentNegotiated(
	jsonHandler http.HandlerFunc,
	feeds feedHandlers,
	spa http.Handler,
) http.HandlerFunc {
	return contentNegotiatedWithSSE(jsonHandler, nil, feeds, spa)
}

// contentNegotiatedWithSSE is contentNegotiated with a stream. A nil
// sseHandler is the endpoint that has none.
//
// Anything the endpoint does not serve — a graph format, or a type nothing
// serves — gets 406, since negotiate resolves without refusing.
func contentNegotiatedWithSSE(
	jsonHandler, sseHandler http.HandlerFunc,
	feeds feedHandlers,
	spa http.Handler,
) http.HandlerFunc {
	// What this endpoint serves is decided here, once, from the handlers it
	// was given, so the switch and the 406 that names its arms cannot drift.
	served := feeds.servedTypes(sseHandler != nil)

	return func(w http.ResponseWriter, r *http.Request) {
		switch ContentTypeFromContext(r.Context()) {
		case ContentTypeHTML:
			addFeedLinks(w, r, feeds)
			spa.ServeHTTP(w, r)
		case ContentTypeSSE:
			if sseHandler == nil {
				writeErrorCode(w, r, "API001", "this endpoint does not support text/event-stream")
				return
			}

			sseHandler(w, r)
		case ContentTypeAtom:
			dispatchFeed(w, r, feeds.atom, served)
		case ContentTypeJSONFeed:
			dispatchFeed(w, r, feeds.jsonFeed, served)
		case ContentTypeCSV:
			if !feeds.csv {
				notAcceptable(w, r, served)
				return
			}

			jsonHandler(w, r)
		case ContentTypeYAML:
			dispatchFeed(w, r, feeds.yaml, served)
		case ContentTypeJSON:
			addFeedLinks(w, r, feeds)
			jsonHandler(w, r)
		default:
			notAcceptable(w, r, served)
		}
	}
}

// dispatchFeed calls the given feed handler, or refuses as the default arm
// does: an endpoint with no feed to serve is one that does not serve the type.
func dispatchFeed(
	w http.ResponseWriter,
	r *http.Request,
	handler http.HandlerFunc,
	served string,
) {
	if handler == nil {
		notAcceptable(w, r, served)
		return
	}
	handler(w, r)
}

func (h *Handlers) streamList(w http.ResponseWriter, r *http.Request, typ cache.EventType) {
	typMatch := sse.TypeMatcher(typ)
	h.broadcaster.ServeSSE(w, r, h.aclMatchWrap(r, typMatch), typ)
}

func (h *Handlers) streamResource(
	w http.ResponseWriter, r *http.Request, typ cache.EventType, id string,
) {
	resMatch := sse.ResourceMatcher(typ, id)
	// No replay for per-resource streams: cross-resource matchers can't be
	// reconstructed from history, so reconnects fall back to a sync event.
	h.broadcaster.ServeSSE(w, r, h.aclMatchWrap(r, resMatch), "")
}

// aclMatchWrap wraps an SSE match function with an ACL authorization check.
// Events that pass the type/resource matcher are further filtered by ACL.
// Sync events always pass through.
func (h *Handlers) aclMatchWrap(
	r *http.Request,
	inner func(cache.Event) bool,
) func(cache.Event) bool {
	id := auth.IdentityFromContext(r.Context())
	return func(ev cache.Event) bool {
		if inner != nil && !inner(ev) {
			return false
		}
		// Sync events carry no resource payload — they signal a full cache
		// refresh (periodic or reconnect). Always pass them through so clients
		// refetch via the JSON endpoint, which IS ACL-filtered. Blocking sync
		// events would cause stale client state with no way to recover.
		if ev.Type == cache.EventSync {
			return true
		}
		return h.acl.Can(id, "read", string(ev.Type)+":"+ev.Name)
	}
}

// addFeedLinks sets Link headers advertising feed alternates (RFC 8288). The
// href carries only the parameters the feed it points at reads, through the
// same feedQuery the feed's own links use.
func addFeedLinks(w http.ResponseWriter, r *http.Request, feeds feedHandlers) {
	if feeds.atom == nil && feeds.jsonFeed == nil && !feeds.csv {
		return
	}

	basePath := absPath(r.Context(), r.URL.Path)

	if feeds.csv {
		w.Header().Add("Link", fmt.Sprintf(
			`<%s>; rel="alternate"; type="text/csv"`,
			feedHref(basePath+".csv", keptQuery(r, feeds.csvParams)),
		))
	}

	if feeds.atom == nil && feeds.jsonFeed == nil {
		return
	}

	query := feedQuery(r, feeds.queryParams)

	if feeds.atom != nil {
		w.Header().Add("Link", fmt.Sprintf(
			`<%s>; rel="alternate"; type="application/atom+xml"`,
			feedHref(basePath+".atom", query),
		))
	}

	if feeds.jsonFeed != nil {
		w.Header().Add("Link", fmt.Sprintf(
			`<%s>; rel="alternate"; type="application/feed+json"`,
			feedHref(basePath+".feed", query),
		))
	}
}
