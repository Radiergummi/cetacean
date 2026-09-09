package api

import (
	"net/http"

	json "github.com/goccy/go-json"
)

// precond evaluates RFC 9110 §13.1.1 If-Match against the representation a GET
// at this same URI would return. It is optional everywhere: a request without
// the header passes straight through, so no existing client is affected.
//
// Comparing against the same-URI representation is what makes this actually
// If-Match rather than a bespoke precondition — and it is what gives networks,
// volumes, tasks, stacks and plugins a working precondition despite carrying
// no Docker version at all.
func (h *Handlers) precond(rep representationFunc) Constructor {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ifMatch := r.Header.Get("If-Match")
			if ifMatch == "" {
				next.ServeHTTP(w, r)
				return
			}

			value, ok := rep(r)
			if !ok {
				// RFC 9110 §13.2.2: no current representation means the
				// precondition fails. 412, not 404 — evaluating the condition
				// comes first.
				writeErrorCode(w, r, "API013",
					"the resource has no current representation")
				return
			}

			body, err := json.Marshal(value)
			if err != nil {
				writeErrorCode(w, r, "API009", "failed to serialize response")
				return
			}

			if !etagMatchStrong(ifMatch, computeETag(body)) {
				writeErrorCode(w, r, "API013",
					"If-Match did not match the current state of the resource")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
