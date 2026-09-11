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
// at this same URI would return. It is optional everywhere: a request without
// the header passes straight through. Comparing against the same-URI
// representation is also what gives networks, volumes, tasks, stacks and
// plugins a precondition, despite carrying no Docker version at all.
//
// The subject is made current from the engine first. Every representation is
// built from the cache, which the watcher fills asynchronously, while every
// writer in internal/docker applies against the engine — so without the
// refresh the header could only ever refuse a change the cache had already
// seen, which is never the lost update it exists to prevent.
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

// preconditionSubjects names, per resource root, the engine record a
// precondition on that root must be evaluated against, and the path value
// holding its identifier.
//
// Stacks are absent because a stack is derived from the labels of the services
// composing it and has no engine record to read; plugins are absent because
// pluginRepresentation already inspects the daemon on demand.
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
// representation the precondition is about to build describes the engine
// rather than whatever the event stream has delivered so far.
//
// The subject comes from the path rather than from a per-route argument: every
// preconditioned route lives under its resource's own root, so one rule covers
// all of them and a route added later is covered without being wired up.
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
