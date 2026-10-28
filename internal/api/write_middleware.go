package api

import (
	"net/http"
	"strconv"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
)

// levelFor is the operations tier this caller may reach. A token is a
// credential left on a device, so a deployment may hold it below the tier it
// runs at; anything else is the deployment's own.
func (h *Handlers) levelFor(r *http.Request) config.OperationsLevel {
	if auth.IdentityFromContext(r.Context()).FromToken() {
		return h.tokenOperationsLevel
	}

	return h.operationsLevel
}

// requireLevel refuses an operation the caller's tier does not reach. The
// comparison is per request rather than per route: two callers on one route can
// stand at different tiers.
func (h *Handlers) requireLevel(required config.OperationsLevel) Constructor {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if level := h.levelFor(r); level >= required {
				next.ServeHTTP(w, r)

				return
			}

			problem.Write(w, r, "OPS001",
				"this operation requires operations level "+strconv.Itoa(int(required))+
					", but the caller is at level "+strconv.Itoa(int(h.levelFor(r))))
		})
	}
}

// attributeWrite records the caller as the author of the write. The transport
// is what the browser says about itself, not a credential: a bearer token is
// never the dashboard's, and only a browser sends Sec-Fetch-Site unprompted.
func attributeWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(cluster.Attributed(r.Context(), writeTransport(r))))
	})
}

func writeTransport(r *http.Request) cache.Transport {
	if auth.ExtractBearerToken(r) == "" && r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		return cache.TransportDashboard
	}

	return cache.TransportREST
}

// requireWriteACL returns middleware that checks ACL write permission for a
// resource. The resourceFunc extracts the resource string (e.g. "service:name")
// from the request.
func (h *Handlers) requireWriteACL(
	resourceFunc func(*http.Request) string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resource := resourceFunc(r)
			id := auth.IdentityFromContext(r.Context())
			if !h.acl.Can(id, "write", resource) {
				problem.Write(w, r, "ACL002", "write access denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// --- ACL resource resolvers ---

// writeResource returns a resolver naming the resource a write to the kind
// the "id" path value addresses is checked against, or "kind:id" for one the
// cache does not hold.
func (h *Handlers) writeResource(kind string) func(*http.Request) string {
	return func(r *http.Request) string {
		id := r.PathValue("id")

		// Only a task addressed by name consults the reader, so the caller's
		// grants are collected for that alone.
		var checker func(string) bool
		canRead := func(resource string) bool {
			if checker == nil {
				checker = h.acl.Checker(auth.IdentityFromContext(r.Context()), "read")
			}

			return checker(resource)
		}

		if record, found, _ := cluster.LookupAs(h.cache, kind, id, canRead); found {
			return record.WriteResource()
		}

		return kind + ":" + id
	}
}

// pathResource returns a resolver that reads the named path value directly.
func pathResource(resourceType, pathKey string) func(*http.Request) string {
	return func(r *http.Request) string {
		return resourceType + ":" + r.PathValue(pathKey)
	}
}

func wildcardResource(resourceType string) func(*http.Request) string {
	return func(_ *http.Request) string {
		return resourceType + ":*"
	}
}

func swarmResource(_ *http.Request) string {
	return "swarm:cluster"
}

// nodeResource returns a consistent ACL resource string for a node.
// Prefers "node:<hostname>", falls back to "node:<id>" if hostname is empty.
func nodeResource(n swarm.Node) string {
	return "node:" + cluster.NodeACLName(n)
}
