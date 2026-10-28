package api

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

// caller is the request's caller as the write use cases take it.
func (h *Handlers) caller(r *http.Request) cluster.Caller {
	identity := auth.IdentityFromContext(r.Context())

	return cluster.Caller{
		CanRead:  h.acl.Checker(identity, "read"),
		CanWrite: h.acl.Checker(identity, "write"),
		Level:    h.levelFor(r),
	}
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
			if h.acl.Can(id, "write", resource) {
				next.ServeHTTP(w, r)
				return
			}

			if kind, key, hidden := h.hiddenTarget(r); hidden {
				problem.Write(w, r, notFoundCodes[kind], fmt.Sprintf("%s %q not found", kind, key))
				return
			}

			problem.Write(w, r, "ACL002", "write access denied")
		})
	}
}

// hiddenTarget reports a write to a record the caller cannot read, which is
// answered as not found so the refusal does not confirm it exists. The record
// is the one the route addresses by {id} or {name}, whatever its write is
// checked against; a key naming several is refused as it was.
func (h *Handlers) hiddenTarget(r *http.Request) (kind, key string, hidden bool) {
	key = cmp.Or(r.PathValue("id"), r.PathValue("name"))
	_, path, _ := strings.Cut(r.Pattern, " /")
	collection, _, _ := strings.Cut(path, "/")
	kind = strings.TrimSuffix(collection, "s")

	if _, ok := notFoundCodes[kind]; !ok || key == "" {
		return kind, key, false
	}

	canRead := h.acl.Checker(auth.IdentityFromContext(r.Context()), "read")
	if kind == "plugin" {
		return kind, key, h.pluginHidden(r, key, canRead)
	}
	if !cluster.Resolvable(kind) {
		return kind, key, false
	}

	record, found, err := cluster.LookupAs(h.cache, kind, key, canRead)
	if err != nil {
		return kind, key, false
	}

	return kind, key, !found || !canRead(record.Resource())
}

// pluginHidden is hiddenTarget for a plugin, which the cache does not hold:
// the daemon is asked only once the caller may read the name.
func (h *Handlers) pluginHidden(r *http.Request, name string, canRead func(string) bool) bool {
	if !canRead("plugin:" + name) {
		return true
	}
	if h.pluginClient == nil {
		return false
	}

	_, err := h.pluginRepresentation(r)

	return errors.Is(err, errNoRepresentation)
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
