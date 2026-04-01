package api

import (
	"net/http"
	"strconv"

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

// --- Name resolvers for ACL resource strings ---

func (h *Handlers) serviceName(r *http.Request) string {
	id := r.PathValue("id")
	if svc, ok := h.cache.GetService(id); ok {
		return "service:" + svc.Spec.Name
	}
	return "service:" + id
}

func (h *Handlers) nodeName(r *http.Request) string {
	id := r.PathValue("id")
	if n, ok := h.cache.GetNode(id); ok {
		if n.Description.Hostname != "" {
			return "node:" + n.Description.Hostname
		}
	}
	return "node:" + id
}

func (h *Handlers) taskServiceResource(r *http.Request) string {
	id := r.PathValue("id")
	if t, ok := h.cache.GetTask(id); ok {
		if svc, ok := h.cache.GetService(t.ServiceID); ok {
			return "service:" + svc.Spec.Name
		}
	}
	return "task:" + id
}

func (h *Handlers) configName(r *http.Request) string {
	id := r.PathValue("id")
	if cfg, ok := h.cache.GetConfig(id); ok {
		return "config:" + cfg.Spec.Name
	}
	return "config:" + id
}

func (h *Handlers) secretName(r *http.Request) string {
	id := r.PathValue("id")
	if s, ok := h.cache.GetSecret(id); ok {
		return "secret:" + s.Spec.Name
	}
	return "secret:" + id
}

func (h *Handlers) networkName(r *http.Request) string {
	id := r.PathValue("id")
	if n, ok := h.cache.GetNetwork(id); ok {
		return "network:" + n.Name
	}
	return "network:" + id
}

func (h *Handlers) stackName(r *http.Request) string {
	return "stack:" + r.PathValue("name")
}

func (h *Handlers) volumeName(r *http.Request) string {
	return "volume:" + r.PathValue("name")
}

func (h *Handlers) pluginName(r *http.Request) string {
	return "plugin:" + r.PathValue("name")
}

func swarmResource(_ *http.Request) string {
	return "swarm:cluster"
}

func wildcardResource(resourceType string) func(*http.Request) string {
	return func(_ *http.Request) string {
		return resourceType + ":*"
	}
}
