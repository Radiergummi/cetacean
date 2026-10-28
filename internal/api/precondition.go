package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/docker"
)

// precond evaluates RFC 9110 §13.1.1 If-Match against the representation a GET
// at the same URI would return, which is what gives networks, volumes, tasks,
// stacks and plugins a precondition at all. The subject is made current from
// the engine first, or the header could only refuse what the cache already saw.
func (h *Handlers) precond(rep representationFunc) Constructor {
	return h.conditional(rep, true, nil)
}

// precondTimed is precond for a representation that also sends Last-Modified,
// which modified reads, so If-Unmodified-Since has a date to compare.
func (h *Handlers) precondTimed(
	rep representationFunc,
	modified func(*http.Request) time.Time,
) Constructor {
	return h.conditional(rep, true, modified)
}

// unrepresented evaluates If-Match on a write whose path serves no GET. There is
// no representation to match, so §13.1.1 makes every If-Match false, "*" too;
// exists only says whether the subject is there, so a missing one still 404s.
func (h *Handlers) unrepresented(exists representationFunc) Constructor {
	return h.conditional(exists, false, nil)
}

// subjectExists is an exists for a write whose subject is the cluster itself.
func subjectExists(*http.Request) (any, error) { return struct{}{}, nil }

// conditional evaluates a write's preconditions in RFC 9110 §13.2.2 order;
// If-Modified-Since binds only a GET or HEAD, so it is not read here.
func (h *Handlers) conditional(
	rep representationFunc,
	represented bool,
	lastModified func(*http.Request) time.Time,
) Constructor {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ifMatch := r.Header.Get("If-Match")
			ifNoneMatch := r.Header.Get("If-None-Match")
			if ifMatch == "" && ifNoneMatch == "" && r.Header.Get("If-Unmodified-Since") == "" {
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
					problem.Write(w, r, "ENG001", err.Error())
					return
				}

				problem.Write(w, r, "ENG004",
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
					problem.Write(w, r, "ENG001", err.Error())
					return
				}

				problem.Write(w, r, "ENG004",
					"failed to read the current representation")
				return
			}

			// With no representation, If-None-Match finds none to match and
			// If-Unmodified-Since has no date, so only If-Match can fail.
			if !represented {
				if ifMatch != "" {
					problem.Write(
						w,
						r,
						"API013",
						"If-Match cannot match: this endpoint serves no representation to compare it to",
					)
					return
				}

				next.ServeHTTP(w, r)
				return
			}

			body, err := json.Marshal(value)
			if err != nil {
				problem.Write(w, r, "API009", "failed to serialize response")
				return
			}
			etag := computeETag(body)

			var modified time.Time
			if lastModified != nil {
				modified = lastModified(r)
			}

			if reason := preconditionFailed(r, etag, modified); reason != "" {
				problem.Write(w, r, "API013", reason)
				return
			}

			if ifNoneMatch != "" && etagMatch(ifNoneMatch, etag) {
				problem.Write(w, r, "API013",
					"If-None-Match matched the current state of the resource")
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

	subject, ok := preconditionSubjects[root]
	if !ok || !cluster.Versioned(subject.kind) {
		return unchanged
	}

	id, version, found := h.cachedVersion(subject.kind, r.PathValue(subject.key))
	if !found {
		return unchanged
	}

	return func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, preconditionedKey{}, true)

		return docker.WithPinnedVersion(ctx, subject.kind, id, version)
	}
}

// cachedVersion reads the ID and version of a record of a Versioned kind.
func (h *Handlers) cachedVersion(kind, key string) (string, swarm.Version, bool) {
	var (
		meta  swarm.Meta
		id    string
		found bool
	)

	switch kind {
	case "service":
		var svc swarm.Service
		svc, found = h.cache.GetService(key)
		id, meta = svc.ID, svc.Meta
	case "node":
		var node swarm.Node
		node, found = h.cache.GetNode(key)
		id, meta = node.ID, node.Meta
	case "config":
		var cfg swarm.Config
		cfg, found = h.cache.GetConfig(key)
		id, meta = cfg.ID, cfg.Meta
	case "secret":
		var sec swarm.Secret
		sec, found = h.cache.GetSecret(key)
		id, meta = sec.ID, sec.Meta
	}

	return id, meta.Version, found
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
