package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// contentNegotiated wraps a JSON handler to dispatch based on content type.
// HTML requests go to the SPA, SSE gets 406 (not supported here).
// Unsupported types are already rejected by the negotiate middleware.
func contentNegotiated(jsonHandler http.HandlerFunc, spa http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch ContentTypeFromContext(r.Context()) {
		case ContentTypeHTML:
			spa.ServeHTTP(w, r)
		case ContentTypeSSE:
			writeErrorCode(w, r, "API001", "this endpoint does not support text/event-stream")
		default:
			jsonHandler(w, r)
		}
	}
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

// contentNegotiatedWithSSE is like contentNegotiated but allows SSE.
func contentNegotiatedWithSSE(
	jsonHandler, sseHandler http.HandlerFunc,
	spa http.Handler,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch ContentTypeFromContext(r.Context()) {
		case ContentTypeHTML:
			spa.ServeHTTP(w, r)
		case ContentTypeSSE:
			sseHandler(w, r)
		default:
			jsonHandler(w, r)
		}
	}
}
