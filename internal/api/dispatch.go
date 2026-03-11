package api

import "net/http"

// contentNegotiated wraps a JSON handler to dispatch based on content type.
// HTML requests go to the SPA, unsupported types get 406.
func contentNegotiated(jsonHandler http.HandlerFunc, spa http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		switch ct {
		case ContentTypeHTML:
			spa.ServeHTTP(w, r)
		case ContentTypeSSE:
			writeProblem(w, r, http.StatusNotAcceptable, "this endpoint does not support text/event-stream")
		default:
			jsonHandler(w, r)
		}
	}
}

// contentNegotiatedWithSSE is like contentNegotiated but allows SSE.
func contentNegotiatedWithSSE(jsonHandler, sseHandler http.HandlerFunc, spa http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		switch ct {
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
		ct := ContentTypeFromContext(r.Context())
		switch ct {
		case ContentTypeHTML:
			spa.ServeHTTP(w, r)
		case ContentTypeSSE:
			sseHandler.ServeHTTP(w, r)
		default:
			writeProblem(w, r, http.StatusNotAcceptable, "this endpoint only supports text/event-stream")
		}
	}
}
