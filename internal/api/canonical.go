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
// keeps its leading slash so it can be concatenated back on unchanged.
// A single trailing slash is dropped rather than carried into the redirect:
// `/services/shop_web/` used to produce `Location: /services/<id>/`, which
// matches no registered pattern — Go's ServeMux treats the trailing slash as
// part of the path — so it fell to the SPA handler and answered a JSON client
// with index.html.
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

// canonicalIdentifier answers a request that addresses a resource by name with
// a 307 to the same path spelled with the resource's canonical ID.
//
// It exists because the two transports disagreed about what an identifier
// means: internal/mcp resolves through cache.Resolve*, which tries the ID and
// then scans names, while REST looked up the ID-keyed map alone — so
// `GET /services/shop_web` answered 404 while the equivalent MCP read
// succeeded, and the note on cache/resolve.go claiming both transports cannot
// disagree was false. Redirecting rather than serving the resource under the
// name keeps one URL per resource: ETags, `@id`, Link headers and the history
// feed all continue to name the ID, and a client that follows the redirect
// lands on the representation it would have got by addressing the ID itself.
//
// 307, specifically: it preserves the method and the body, so the same rule can
// cover writes — `PUT /services/shop_web/scale` reaches the scale handler with
// its payload intact. 308 would be wrong because it is cacheable indefinitely
// and a name can be moved to another resource; 301 and 302 would be worse still,
// since clients are permitted to rewrite the method to GET.
//
// It deliberately does nothing for HTML. Those requests are the SPA's own
// routing surface, and the dashboard's URLs are its to decide.
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
			// The report names every candidate ID, so it is a disclosure in
			// its own right. A caller who could not read any resource of this
			// type is told nothing and falls through to the handler, which
			// answers the unresolved name with its ordinary 404. The question
			// is type-level because an ambiguous name resolves to no single
			// resource to check, and acl.TypeGrants answers it from one policy
			// read.
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

		// A redirect is a statement that the named resource exists, and it
		// hands over its ID. Answering it for a caller with no read grant
		// would turn every detail path into a way to enumerate names and
		// discover IDs behind the policy, so the request falls through to the
		// handler instead, which answers exactly as it does for the ID.
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
