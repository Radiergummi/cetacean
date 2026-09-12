package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// canonicalResolver resolves one resource type's identifier — an ID or a name
// — to the canonical ID the type is keyed by, together with the ACL resource
// expression naming what it found. It reports found=false for an identifier
// that matches nothing, and an *cache.AmbiguousNameError for a name that
// matches more than one resource.
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

// canonicalResolvers lists the resource collections whose detail paths accept
// a name as well as an ID.
//
// Volumes and stacks are deliberately absent: both are keyed by name already,
// so the identifier in the path is the canonical one and there is nothing to
// redirect to. Tasks are absent too — a task has no name of its own, only the
// `<service>.<slot>` form internal/cluster derives from its parent, and the
// cache has no resolver for it (internal/mcp builds that one from its own
// resolveTask). Adding tasks means giving the cache that resolver first, so
// both transports keep agreeing on what a task identifier means.
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
// identifier addressing one member of it, and whatever follows. The remainder
// keeps its leading slash so it concatenates back unchanged. A single trailing
// slash is dropped: ServeMux treats it as part of the path, so a redirect
// carrying one matches no registered pattern.
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

// canonicalIdentifier answers a request addressing a resource by name with a
// 307 to the same path spelled with the canonical ID, so one resource keeps one
// URL -- the one ETags, `@id`, Link headers and the history feed all name.
//
// 307 preserves the method and body, so writes may be addressed by name too.
// 308 is cacheable indefinitely and a name can move; 301 and 302 let clients
// rewrite the method to GET.
//
// HTML requests are left alone: those paths are the SPA's routing surface.
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

		target := absPath(r.Context(), "/"+collection+"/"+url.PathEscape(id)+rest)
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}

		w.Header().Set("Location", target)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
}
