package api

import (
	"fmt"
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

// routedWrites matches r against the routes once. methods lists GET, HEAD and
// every write method routed at the matched path that the caller's level
// reaches; below names the sub-resources directly under it that route one.
func (h *Handlers) routedWrites(r *http.Request) (methods, below []string) {
	routes := routeTable()
	_, pattern := routes.mux.Handler(r)
	_, path, routed := strings.Cut(pattern, " ")
	methods = []string{"GET", "HEAD"}
	if !routed {
		return methods, nil
	}
	level := h.levelFor(r)

	offered := make(map[string]bool)
	for _, route := range routes.tiered {
		if level < route.level || !slices.Contains(writeMethods, route.method) {
			continue
		}
		if route.path == path {
			offered[route.method] = true
		} else if name, ok := strings.CutPrefix(route.path, path+"/"); ok &&
			!strings.ContainsAny(name, "/{") && !slices.Contains(below, name) {
			below = append(below, name)
		}
	}

	for _, method := range writeMethods {
		if offered[method] {
			methods = append(methods, method)
		}
	}

	return methods, below
}

// acceptPatch is the Accept-Patch of a path that takes PATCH: labels and
// service env apply JSON Patch to a map, every other patch is a merge.
func acceptPatch(path string) string {
	switch path[strings.LastIndex(path, "/")+1:] {
	case "labels", "env":
		return AcceptPatch
	default:
		return AcceptMergePatch
	}
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

// setAllow sets a detail endpoint's Allow from the operations level and the
// caller's write grant, and links each sub-resource the caller may write.
func (h *Handlers) setAllow(
	w http.ResponseWriter,
	r *http.Request,
	resourceType, resourceName string,
) {
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "write", resourceType+":"+resourceName) {
		writeAllow(w, r, []string{"GET", "HEAD"})
		return
	}

	methods, below := h.routedWrites(r)
	writeAllow(w, r, methods)

	self := absPath(r.Context(), r.URL.EscapedPath())
	var links []string
	for _, name := range below {
		links = append(links, fmt.Sprintf(`<%s/%s>; rel="edit"`, self, name))
	}
	if len(links) > 0 {
		w.Header().Add("Link", strings.Join(links, ", "))
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
		methods, _ = h.routedWrites(r)
	}
	writeAllow(w, r, methods)
}

// writeAllow writes Allow, and Accept-Patch when the methods take a PATCH.
func writeAllow(w http.ResponseWriter, r *http.Request, methods []string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	varyByIdentity(w)

	if slices.Contains(methods, "PATCH") {
		w.Header().Set("Accept-Patch", acceptPatch(r.URL.Path))
	}
}

// createsByName lists the types whose create checks the write grant on the
// created name, so any write grant on the type may allow one; the rest need it
// type-wide.
var createsByName = map[string]bool{"config": true, "secret": true}

// setAllowList sets the Allow header for list endpoints.
func (h *Handlers) setAllowList(w http.ResponseWriter, r *http.Request, resourceType string) {
	methods := []string{"GET", "HEAD"}
	id := auth.IdentityFromContext(r.Context())
	if createsByName[resourceType] && h.acl.TypeGrants(id).Can("write", resourceType) ||
		h.acl.Can(id, "write", resourceType+":*") {
		methods, _ = h.routedWrites(r)
	}
	writeAllow(w, r, methods)
}
