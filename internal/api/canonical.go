package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// canonicalResolver resolves one type's identifier — an ID or a name — to the
// canonical ID it is keyed by, with the ACL expression naming what it found.
// found=false means nothing matched; a name matching several yields an
// *cache.AmbiguousNameError.
type canonicalResolver func(
	c *cache.Cache,
	identifier string,
) (id string, aclResource string, found bool, err error)

// singularType maps a collection segment to the ACL resource type its members
// carry, for the coarse "could this identity read anything of this type?"
// question an ambiguity report has to answer before it names candidates.
var singularType = map[string]string{
	"services": "service",
	"nodes":    "node",
	"configs":  "config",
	"secrets":  "secret",
	"networks": "network",
}

// canonicalResolvers lists the collections whose detail paths accept a name as
// well as an ID. Volumes and stacks are absent because both are keyed by name
// already; tasks because a task's name is derived from its parent and the
// cache has no resolver for it. Adding tasks means adding that resolver first.
var canonicalResolvers = map[string]canonicalResolver{
	"services": func(c *cache.Cache, identifier string) (string, string, bool, error) {
		svc, found, err := c.ResolveService(identifier)

		return svc.ID, "service:" + svc.Spec.Name, found, err
	},
	"nodes": func(c *cache.Cache, identifier string) (string, string, bool, error) {
		node, found, err := c.ResolveNode(identifier)

		return node.ID, nodeResource(node), found, err
	},
	"configs": func(c *cache.Cache, identifier string) (string, string, bool, error) {
		cfg, found, err := c.ResolveConfig(identifier)

		return cfg.ID, "config:" + cfg.Spec.Name, found, err
	},
	"secrets": func(c *cache.Cache, identifier string) (string, string, bool, error) {
		sec, found, err := c.ResolveSecret(identifier)

		return sec.ID, "secret:" + sec.Spec.Name, found, err
	},
	"networks": func(c *cache.Cache, identifier string) (string, string, bool, error) {
		net, found, err := c.ResolveNetwork(identifier)

		return net.ID, "network:" + net.Name, found, err
	},
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

		resolve, ok := canonicalResolvers[collection]
		if !ok || identifier == "" {
			next.ServeHTTP(w, r)

			return
		}

		id, resource, found, err := resolve(h.cache, identifier)

		var ambiguous *cache.AmbiguousNameError
		if errors.As(err, &ambiguous) {
			// The report names every candidate ID, so it is a disclosure.
			// Type-level, because an ambiguous name resolves to no single
			// resource to check.
			access := h.acl.TypeGrants(auth.IdentityFromContext(r.Context()))
			if !access.Can("read", singularType[collection]) {
				next.ServeHTTP(w, r)

				return
			}

			writeErrorCode(w, r, "API014", ambiguous.Error())

			return
		}

		// Either the identifier is already the canonical ID — the common case,
		// and the one that must stay cheap — or it names nothing, in which
		// case the handler's own 404 says so with the wording its callers
		// already expect.
		if !found || id == identifier {
			next.ServeHTTP(w, r)

			return
		}

		// A redirect states the named resource exists and hands over its ID,
		// so without a read grant the request falls through to the handler.
		if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", resource) {
			next.ServeHTTP(w, r)

			return
		}

		// The extension suffix goes back on: negotiate stripped it, and a
		// request naming its representation in the path has no reason to
		// repeat it in Accept — so dropping it answers the redirect from
		// whatever Accept does say, which for a browser is the SPA.
		target := absPath(r.Context(), "/"+collection+"/"+url.PathEscape(id)+rest) +
			extensionFromContext(r.Context())
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}

		w.Header().Set("Location", target)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
}
