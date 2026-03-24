package api

import "net/http"

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

// sseOnly is for endpoints that only support SSE (like /events).
func sseOnly(sseHandler http.Handler, spa http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch ContentTypeFromContext(r.Context()) {
		case ContentTypeHTML:
			spa.ServeHTTP(w, r)
		case ContentTypeSSE:
			sseHandler.ServeHTTP(w, r)
		default:
			writeErrorCode(w, r, "API002", "this endpoint only supports text/event-stream")
		}
	}
}
