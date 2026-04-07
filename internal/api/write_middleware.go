package api

import (
	"net/http"
	"strconv"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// requireLevel returns middleware that blocks requests when the configured
// operations level is below the required level for this endpoint.
func requireLevel(required, configured config.OperationsLevel) func(http.HandlerFunc) http.Handler {
	return func(next http.HandlerFunc) http.Handler {
		if configured >= required {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeErrorCode(w, r, "OPS001",
				"this operation requires operations level "+strconv.Itoa(int(required))+
					", but the server is configured at level "+strconv.Itoa(int(configured)))
		})
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
			if !h.acl.Can(id, "write", resource) {
				writeErrorCode(w, r, "ACL002", "write access denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// --- ACL resource resolvers ---

// resolveResource returns a resolver that looks up a resource by the "id" path
// value, extracts its name, and returns "type:name" (falling back to "type:id").
func resolveResource[T any](
	resourceType string,
	get func(string) (T, bool),
	name func(T) string,
) func(*http.Request) string {
	return func(r *http.Request) string {
		id := r.PathValue("id")
		if obj, ok := get(id); ok {
			return resourceType + ":" + name(obj)
		}

		return resourceType + ":" + id
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
	return "node:" + nodeHostnameOrID(n)
}

// nodeHostnameOrID returns the hostname if set, otherwise the ID.
func nodeHostnameOrID(n swarm.Node) string {
	if n.Description.Hostname != "" {
		return n.Description.Hostname
	}

	return n.ID
}

// taskServiceResource resolves a task to its parent service for ACL checks.
func (h *Handlers) taskServiceResource(r *http.Request) string {
	id := r.PathValue("id")
	if t, ok := h.cache.GetTask(id); ok {
		if svc, ok := h.cache.GetService(t.ServiceID); ok {
			return "service:" + svc.Spec.Name
		}
		// Task found but service missing (orphaned): preserve service type
		// so service:* grants still match via the evaluator.
		return "service:" + t.ServiceID
	}

	return "task:" + id
}
