package api

import (
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// AcceptPatch is the Accept-Patch header value for endpoints that support both
// JSON Patch (RFC 6902) and JSON Merge Patch (RFC 7396).
const AcceptPatch = "application/json-patch+json, application/merge-patch+json"

// AcceptMergePatch is the Accept-Patch header value for endpoints that only
// support JSON Merge Patch (RFC 7396).
const AcceptMergePatch = "application/merge-patch+json"

// tieredChain is a write route's middleware at one operations level. Its
// handlers carry that level, so the router records what each write route needs
// and Allow is read off the routes instead of a list kept beside them.
type tieredChain struct {
	chain Chain
	level config.OperationsLevel
}

func (h *Handlers) tiered(acl Constructor, level config.OperationsLevel) tieredChain {
	return tieredChain{NewChain(acl, h.requireLevel(level)), level}
}

func (c tieredChain) Append(constructors ...Constructor) tieredChain {
	return tieredChain{c.chain.Append(constructors...), c.level}
}

func (c tieredChain) ThenFunc(fn http.HandlerFunc) http.Handler {
	return tieredHandler{c.chain.ThenFunc(fn), c.level}
}

type tieredHandler struct {
	http.Handler
	level config.OperationsLevel
}

type tieredRoute struct {
	method, path string
	level        config.OperationsLevel
}

// routeTable is a router built without dependencies, for its routes alone.
// Which routes exist and the level each needs does not vary with configuration,
// and handlers called without a router in front of them answer Allow as well.
func routeTable() *routeRecorder {
	routeTableOnce.Do(func() {
		_, routeTableRoutes = newRouter(RouterConfig{
			Handlers:     &Handlers{},
			AuthProvider: &auth.NoneProvider{},
			OpenAPISpec:  []byte("{}"),
			AsyncAPISpec: []byte("{}"),
		})
	})

	return routeTableRoutes
}

var (
	routeTableOnce   sync.Once
	routeTableRoutes *routeRecorder
)

var writeMethods = []string{"PUT", "POST", "PATCH", "DELETE"}

// allowedMethods lists GET, HEAD and every write method routed at the path r
// matched that the caller's level reaches. With below, the routes under that
// path count too: a resource's Allow speaks for its sub-resources' writes.
func (h *Handlers) allowedMethods(r *http.Request, below bool) []string {
	routes := routeTable()
	_, pattern := routes.mux.Handler(r)
	_, path, routed := strings.Cut(pattern, " ")
	level := h.levelFor(r)

	offered := make(map[string]bool)
	for _, route := range routes.tiered {
		at := route.path == path || below && strings.HasPrefix(route.path, path+"/")
		if routed && at && level >= route.level {
			offered[route.method] = true
		}
	}

	methods := []string{"GET", "HEAD"}
	for _, method := range writeMethods {
		if offered[method] {
			methods = append(methods, method)
		}
	}

	return methods
}

// resourceAcceptPatch maps resource types to their Accept-Patch header value.
// Resources with map-patch sub-endpoints (env, labels) accept both JSON Patch
// and JSON Merge Patch; others accept only JSON Merge Patch.
var resourceAcceptPatch = map[string]string{
	"service": AcceptPatch,
	"node":    AcceptPatch,
	"config":  AcceptPatch,
	"secret":  AcceptPatch,
	"swarm":   AcceptMergePatch,
	"plugin":  AcceptMergePatch,
}

const varyIdentity = "Authorization, Cookie"

// varyByIdentity marks a response as per-caller. Every ACL-filtered response
// owes it: the setAllow* seams cover the ones reporting a per-identity Allow,
// requireAnyGrant covers the ones gated on having any, and the feed renderers
// cover themselves. Add rather than Set, to extend a Vary another layer already
// wrote — but only once, since those three overlap.
func varyByIdentity(w http.ResponseWriter) {
	if slices.Contains(w.Header().Values("Vary"), varyIdentity) {
		return
	}

	w.Header().Add("Vary", varyIdentity)
}

// setAllow sets the Allow response header for a detail endpoint based on the
// configured operations level and ACL write permission.
func (h *Handlers) setAllow(
	w http.ResponseWriter,
	r *http.Request,
	resourceType, resourceName string,
) {
	methods := []string{"GET", "HEAD"}

	id := auth.IdentityFromContext(r.Context())
	if h.acl.Can(id, "write", resourceType+":"+resourceName) {
		methods = h.allowedMethods(r, true)
	}

	w.Header().Set("Allow", strings.Join(methods, ", "))
	varyByIdentity(w)

	if slices.Contains(methods, "PATCH") {
		if ap, ok := resourceAcceptPatch[resourceType]; ok {
			w.Header().Set("Accept-Patch", ap)
		}
	}
}

// setAllowSubResource sets the Allow header for a sub-resource endpoint.
func (h *Handlers) setAllowSubResource(
	w http.ResponseWriter,
	r *http.Request,
	resourceExpr string,
) {
	methods := []string{"GET", "HEAD"}
	if h.acl.Can(auth.IdentityFromContext(r.Context()), "write", resourceExpr) {
		methods = h.allowedMethods(r, false)
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	varyByIdentity(w)
}

// setAllowList sets the Allow header for list endpoints.
func (h *Handlers) setAllowList(w http.ResponseWriter, r *http.Request, resourceType string) {
	methods := []string{"GET", "HEAD"}
	if h.acl.Can(auth.IdentityFromContext(r.Context()), "write", resourceType+":*") {
		methods = h.allowedMethods(r, false)
	}

	w.Header().Set("Allow", strings.Join(methods, ", "))
	varyByIdentity(w)
}
