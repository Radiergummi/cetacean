package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	json "github.com/goccy/go-json"
)

// precond evaluates RFC 9110 §13.1.1 If-Match against the representation a GET
// at the same URI would return, which is what gives networks, volumes, tasks,
// stacks and plugins a precondition at all. The subject is made current from
// the engine first, or the header could only refuse what the cache already saw.
func (h *Handlers) precond(rep representationFunc) Constructor {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ifMatch := r.Header.Get("If-Match")
			if ifMatch == "" {
				next.ServeHTTP(w, r)
				return
			}

			if err := h.refreshSubject(r); err != nil {
				// Same reasoning as an unbuildable representation below: the
				// condition could not be evaluated, and 412 would name the
				// wrong problem.
				slog.Error("failed to refresh a precondition subject",
					"path", r.URL.Path, "error", err)

				if cerrdefs.IsUnavailable(err) {
					writeErrorCode(w, r, "ENG001", err.Error())
					return
				}

				writeErrorCode(w, r, "ENG004",
					"failed to read the current representation")
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

// preconditionSubjects names, per resource root, the engine record to evaluate
// a precondition against and the path value holding its identifier. Stacks are
// absent because they are derived from service labels and have no engine
// record; plugins because pluginRepresentation already inspects the daemon.
var preconditionSubjects = map[string]struct{ kind, key string }{
	"services": {"service", "id"},
	"nodes":    {"node", "id"},
	"tasks":    {"task", "id"},
	"configs":  {"config", "id"},
	"secrets":  {"secret", "id"},
	"networks": {"network", "id"},
	"volumes":  {"volume", "name"},
}

// refreshSubject re-reads the resource this request addresses, so the
// representation describes the engine rather than what the event stream has
// delivered so far. The subject comes from the path rather than a per-route
// argument, so a route added later is covered without being wired up.
func (h *Handlers) refreshSubject(r *http.Request) error {
	if h.refresher == nil {
		return nil
	}

	root, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")

	subject, ok := preconditionSubjects[root]
	if !ok {
		return nil
	}

	id := r.PathValue(subject.key)
	if id == "" {
		return nil
	}

	return h.refresher.Refresh(r.Context(), subject.kind, id)
}
