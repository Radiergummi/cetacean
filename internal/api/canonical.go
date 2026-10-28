package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// canonicalKinds maps the collections whose detail paths accept a name as well
// as an ID to the kind cluster.Lookup resolves their members as. Volumes and
// stacks are absent because both are keyed by name already.
var canonicalKinds = map[string]string{
	"services": "service",
	"nodes":    "node",
	"tasks":    "task",
	"configs":  "config",
	"secrets":  "secret",
	"networks": "network",
}

// splitResourcePath splits a request path into its collection segment, the
// identifier addressing one member, and the remainder, which keeps its leading
// slash so it concatenates back unchanged. A single trailing slash is dropped:
// ServeMux treats it as part of the path, so a redirect carrying one matches nothing.
func splitResourcePath(path string) (collection, identifier, rest string) {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed != "" {
		trimmed = strings.TrimSuffix(trimmed, "/")
	}

	collection, remainder, _ := strings.Cut(trimmed, "/")
	if remainder == "" {
		return collection, "", ""
	}

	identifier, tail, hasTail := strings.Cut(remainder, "/")
	if hasTail {
		rest = "/" + tail
	}

	return collection, identifier, rest
}

// canonicalIdentifier answers a name-addressed request with a 307 to the same
// path spelled with the canonical ID, so one resource keeps one URL. 307
// preserves method and body; 308 is cacheable forever and a name can move,
// while 301 and 302 let clients rewrite to GET. HTML is left to the SPA.
func (h *Handlers) canonicalIdentifier(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ContentTypeFromContext(r.Context()) == ContentTypeHTML {
			next.ServeHTTP(w, r)

			return
		}

		collection, identifier, rest := splitResourcePath(r.URL.Path)

		kind, ok := canonicalKinds[collection]
		if !ok || identifier == "" {
			next.ServeHTTP(w, r)

			return
		}

		record, found, err := cluster.LookupAs(h.cache, kind, identifier, h.readerFor(r))

		// A task name's ambiguity would be its service's, which Swarm keeps unique.
		if _, ok := errors.AsType[*cache.AmbiguousNameError](err); ok {
			if kind == "task" || !h.reportAmbiguousName(w, r, kind, err) {
				next.ServeHTTP(w, r)
			}

			return
		}

		// Either the identifier is already the canonical ID — the common case,
		// and the one that must stay cheap — or it names nothing, in which
		// case the handler's own 404 says so with the wording its callers
		// already expect.
		if !found || record.ID == identifier {
			next.ServeHTTP(w, r)

			return
		}

		// A redirect states the named resource exists and hands over its ID,
		// so without a read grant the request falls through to the handler.
		if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", record.Resource()) {
			next.ServeHTTP(w, r)

			return
		}

		// The extension suffix goes back on: negotiate stripped it, and a
		// request naming its representation in the path has no reason to
		// repeat it in Accept — so dropping it answers the redirect from
		// whatever Accept does say, which for a browser is the SPA.
		target := absPath(r.Context(), "/"+collection+"/"+url.PathEscape(record.ID)+rest) +
			extensionFromContext(r.Context())
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}

		w.Header().Set("Location", target)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
}

// readerFor is the read check cluster.LookupAs resolves a task name through.
func (h *Handlers) readerFor(r *http.Request) func(resource string) bool {
	identity := auth.IdentityFromContext(r.Context())

	return func(resource string) bool { return h.acl.Can(identity, "read", resource) }
}

// reportAmbiguousName answers a name matching several resources of one type
// with API015, and only for a caller who may read them: the report names every
// candidate ID. Fewer than two survivors is no ambiguity the caller can see, so
// it reports nothing and says so, leaving the request to be answered as it stands.
func (h *Handlers) reportAmbiguousName(
	w http.ResponseWriter,
	r *http.Request,
	singular string,
	err error,
) bool {
	ambiguous, ok := errors.AsType[*cache.AmbiguousNameError](err)
	if !ok {
		return false
	}

	// Every candidate is keyed by the name that was asked for, because that is
	// what a grant matches: a node pattern is compared to its hostname, and an
	// ID only stands in for a node that has none — which an ambiguous one,
	// sharing a hostname with another, never is.
	readable := acl.Filter(
		h.acl, auth.IdentityFromContext(r.Context()), "read", ambiguous.IDs,
		func(string) string { return singular + ":" + ambiguous.Name },
	)
	if len(readable) < 2 {
		return false
	}

	writeErrorCode(w, r, "API015", (&cache.AmbiguousNameError{
		Name: ambiguous.Name,
		IDs:  readable,
	}).Error())

	return true
}
