package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/docker"
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

			// Read before the representation: if the record moves in between,
			// the write is refused rather than let through.
			pinned := h.pinSubjectVersion(r)

			value, err := rep(r)
			switch {
			case errors.Is(err, errNoRepresentation):
				// RFC 9110 §13.2.1: a precondition is ignored when the answer
				// without it would be neither 2xx nor 412, and for a resource
				// that is gone that answer is 404. Only the handler can say
				// so, so the request goes through to it unconditioned.
				next.ServeHTTP(w, r.WithContext(
					context.WithValue(r.Context(), absentSubjectKey{}, true),
				))

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

			next.ServeHTTP(w, r.WithContext(pinned(r.Context())))
		})
	}
}

// preconditionedKey marks a request whose subject's version is pinned, so the
// engine's sequence conflict on it is the precondition failing late.
type preconditionedKey struct{}

func preconditioned(ctx context.Context) bool {
	marked, _ := ctx.Value(preconditionedKey{}).(bool)

	return marked
}

// pinSubjectVersion returns what makes the write name the version of the
// record the precondition is evaluated against, for the kinds whose engine
// write takes one. Deletes take none, so only updates are made atomic.
func (h *Handlers) pinSubjectVersion(r *http.Request) func(context.Context) context.Context {
	unchanged := func(ctx context.Context) context.Context { return ctx }

	root, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	id := r.PathValue("id")

	var (
		kind    string
		meta    swarm.Meta
		subject string
		found   bool
	)

	switch root {
	case "services":
		var svc swarm.Service
		svc, found = h.cache.GetService(id)
		kind, subject, meta = "service", svc.ID, svc.Meta
	case "nodes":
		var node swarm.Node
		node, found = h.cache.GetNode(id)
		kind, subject, meta = "node", node.ID, node.Meta
	case "configs":
		var cfg swarm.Config
		cfg, found = h.cache.GetConfig(id)
		kind, subject, meta = "config", cfg.ID, cfg.Meta
	case "secrets":
		var sec swarm.Secret
		sec, found = h.cache.GetSecret(id)
		kind, subject, meta = "secret", sec.ID, sec.Meta
	}

	if !found {
		return unchanged
	}

	return func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, preconditionedKey{}, true)

		return docker.WithPinnedVersion(ctx, kind, subject, meta.Version)
	}
}

// absentSubjectKey marks a request the precondition let through because there
// was no representation to evaluate it against. Passing through is sound only
// while the handler answers 404 too, and one reading the engine live rather
// than the cache can find the resource back between the two reads.
type absentSubjectKey struct{}

// subjectAbsent reports whether the precondition found the resource gone.
func subjectAbsent(ctx context.Context) bool {
	absent, _ := ctx.Value(absentSubjectKey{}).(bool)

	return absent
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
