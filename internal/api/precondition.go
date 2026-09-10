package api

import (
	"errors"
	"log/slog"
	"net/http"

	cerrdefs "github.com/containerd/errdefs"
	json "github.com/goccy/go-json"
)

// precond evaluates RFC 9110 §13.1.1 If-Match against the representation a GET
// at this same URI would return. It is optional everywhere: a request without
// the header passes straight through. Comparing against the same-URI
// representation is also what gives networks, volumes, tasks, stacks and
// plugins a precondition, despite carrying no Docker version at all.
func (h *Handlers) precond(rep representationFunc) Constructor {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ifMatch := r.Header.Get("If-Match")
			if ifMatch == "" {
				next.ServeHTTP(w, r)
				return
			}

			value, err := rep(r)
			switch {
			case errors.Is(err, errNoRepresentation):
				// RFC 9110 §13.2.2: no current representation means the
				// precondition fails. 412, not 404 — evaluating the condition
				// comes first.
				writeErrorCode(w, r, "API013",
					"the resource has no current representation")
				return
			case err != nil:
				// The condition could not be evaluated at all. Reporting 412
				// here would tell the caller its validator is stale, which is
				// a different problem with a different fix.
				slog.Error("failed to build a precondition representation",
					"path", r.URL.Path, "error", err)

				if cerrdefs.IsUnavailable(err) {
					writeErrorCode(w, r, "ENG001", err.Error())
					return
				}

				writeErrorCode(w, r, "ENG004",
					"failed to read the current representation")
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
