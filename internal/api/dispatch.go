package api

import (
	"fmt"
	"net/http"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// feedHandlers groups the optional feed format handlers for an endpoint.
type feedHandlers struct {
	atom     http.HandlerFunc
	jsonFeed http.HandlerFunc
}

// hasFeed reports whether any feed handler is configured.
func (f feedHandlers) hasFeed() bool {
	return f.atom != nil || f.jsonFeed != nil
}

// contentNegotiated wraps a JSON handler to dispatch based on content type.
// HTML requests go to the SPA, SSE gets 406 (not supported here).
// Unsupported types are already rejected by the negotiate middleware.
func contentNegotiated(
	jsonHandler http.HandlerFunc,
	feeds feedHandlers,
	spa http.Handler,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch ContentTypeFromContext(r.Context()) {
		case ContentTypeHTML:
			addFeedLinks(w, r, feeds)
			spa.ServeHTTP(w, r)
		case ContentTypeSSE:
			writeErrorCode(w, r, "API001", "this endpoint does not support text/event-stream")
		case ContentTypeAtom:
			dispatchFeed(w, r, feeds.atom, "application/atom+xml")
		case ContentTypeJSONFeed:
			dispatchFeed(w, r, feeds.jsonFeed, "application/feed+json")
		default:
			addFeedLinks(w, r, feeds)
			jsonHandler(w, r)
		}
	}
}

// contentNegotiatedWithSSE is like contentNegotiated but allows SSE.
func contentNegotiatedWithSSE(
	jsonHandler, sseHandler http.HandlerFunc,
	feeds feedHandlers,
	spa http.Handler,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch ContentTypeFromContext(r.Context()) {
		case ContentTypeHTML:
			addFeedLinks(w, r, feeds)
			spa.ServeHTTP(w, r)
		case ContentTypeSSE:
			sseHandler(w, r)
		case ContentTypeAtom:
			dispatchFeed(w, r, feeds.atom, "application/atom+xml")
		case ContentTypeJSONFeed:
			dispatchFeed(w, r, feeds.jsonFeed, "application/feed+json")
		default:
			addFeedLinks(w, r, feeds)
			jsonHandler(w, r)
		}
	}
}

// dispatchFeed calls the given feed handler, or returns 406 if nil.
func dispatchFeed(
	w http.ResponseWriter,
	r *http.Request,
	handler http.HandlerFunc,
	mediaType string,
) {
	if handler == nil {
		writeErrorCode(
			w,
			r,
			"API003",
			"this endpoint does not support "+mediaType,
		)
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

// addFeedLinks sets Link headers advertising feed alternates (RFC 8288).
func addFeedLinks(w http.ResponseWriter, r *http.Request, feeds feedHandlers) {
	if !feeds.hasFeed() {
		return
	}

	basePath := absPath(r.Context(), r.URL.Path)
	rq := r.URL.RawQuery

	if feeds.atom != nil {
		href := basePath + ".atom"
		if rq != "" {
			href += "?" + rq
		}
		w.Header().Add("Link", fmt.Sprintf(
			`<%s>; rel="alternate"; type="application/atom+xml"`,
			href,
		))
	}

	if feeds.jsonFeed != nil {
		href := basePath + ".feed"
		if rq != "" {
			href += "?" + rq
		}
		w.Header().Add("Link", fmt.Sprintf(
			`<%s>; rel="alternate"; type="application/feed+json"`,
			href,
		))
	}
}
