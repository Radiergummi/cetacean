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
	chain       Chain
	level       config.OperationsLevel
	acceptPatch string
	noContent   bool
}

func (h *Handlers) tiered(acl Constructor, level config.OperationsLevel) tieredChain {
	return tieredChain{
		chain: NewChain(acl, h.requireLevel(level), attributeWrite),
		level: level,
	}
}

func (c tieredChain) Append(constructors ...Constructor) tieredChain {
	c.chain = c.chain.Append(constructors...)

	return c
}

// TakesJSONPatch marks a PATCH route whose handler parses JSON Patch as well
// as a merge patch; any other PATCH route is advertised as merge-only.
func (c tieredChain) TakesJSONPatch() tieredChain {
	c.acceptPatch = AcceptPatch

	return c
}

// AnswersNoContent marks a write that only ever answers 204, which has no
// representation for Accept to refuse.
func (c tieredChain) AnswersNoContent() tieredChain {
	c.noContent = true

	return c
}

func (c tieredChain) ThenFunc(fn http.HandlerFunc) http.Handler {
	handler := c.chain.ThenFunc(fn)
	if !c.noContent {
		handler = acceptsJSONAnswer(handler)
	}

	return tieredHandler{handler, c.level, c.acceptPatch}
}

// acceptsJSONAnswer refuses a write whose JSON answer the client would not
// accept, before anything runs, unless it asked for no body at all.
func acceptsJSONAnswer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !preferMinimal(r) && !acceptable(r, ContentTypeJSON) {
			notAcceptable(w, r, "application/json")
			return
		}

		next.ServeHTTP(w, r)
	})
}

type tieredHandler struct {
	http.Handler
	level       config.OperationsLevel
	acceptPatch string
}

type tieredRoute struct {
	method, path string
	level        config.OperationsLevel
	acceptPatch  string
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

// routedWrites matches r against the routes once. Its methods list GET, HEAD
// and every write method routed at the matched path that the caller's level
// reaches; below, only when asked for, names the sub-resources directly under
// it that route one.
func (h *Handlers) routedWrites(r *http.Request, withBelow bool) offeredWrites {
	routes := routeTable()
	_, pattern := routes.mux.Handler(r)
	_, path, routed := strings.Cut(pattern, " ")
	offer := offeredWrites{methods: []string{"GET", "HEAD"}}
	if !routed {
		return offer
	}
	level := h.levelFor(r)

	offered := make(map[string]bool)
	below := path + "/"
	for _, route := range routes.tiered {
		if level < route.level || !slices.Contains(writeMethods, route.method) {
			continue
		}
		if route.path == path {
			offered[route.method] = true
			if route.method == http.MethodPatch {
				offer.acceptPatch = route.acceptPatch
			}
		} else if !withBelow {
			continue
		} else if name, ok := strings.CutPrefix(route.path, below); ok &&
			!strings.ContainsAny(name, "/{") && !slices.Contains(offer.below, name) {
			offer.below = append(offer.below, name)
		}
	}

	for _, method := range writeMethods {
		if offered[method] {
			offer.methods = append(offer.methods, method)
		}
	}

	return offer
}

// offeredWrites is what routedWrites found: the methods for Allow, the
// Accept-Patch of the PATCH among them, and the writable sub-resources.
type offeredWrites struct {
	methods, below []string
	acceptPatch    string
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
		writeAllow(w, readOnly)
		return
	}

	offer := h.routedWrites(r, true)
	writeAllow(w, offer)

	self := absPath(r.Context(), r.URL.EscapedPath())
	var links []string
	for _, name := range offer.below {
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
	offer := readOnly
	if h.acl.Can(auth.IdentityFromContext(r.Context()), "write", resourceExpr) {
		offer = h.routedWrites(r, false)
	}
	writeAllow(w, offer)
}

var readOnly = offeredWrites{methods: []string{"GET", "HEAD"}}

// allowReads is the Allow of an endpoint no write is routed at. It does not
// vary by caller, so it adds no Vary; a handler with writes overwrites it.
func allowReads(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, HEAD")
}

// jsonOnly serves next, a read that sends only JSON, to a client that takes it.
func jsonOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !acceptable(r, ContentTypeJSON) {
			notAcceptable(w, r, "application/json")
			return
		}

		allowReads(w)
		next(w, r)
	}
}

// writeAllow writes Allow, and Accept-Patch when the methods take a PATCH.
func writeAllow(w http.ResponseWriter, offer offeredWrites) {
	w.Header().Set("Allow", strings.Join(offer.methods, ", "))
	varyByIdentity(w)

	if offer.acceptPatch != "" {
		w.Header().Set("Accept-Patch", offer.acceptPatch)
	}
}

// createsByName lists the types whose create checks the write grant on the
// created name, so any write grant naming the type may allow one; the rest
// need it type-wide.
var createsByName = map[string]bool{"config": true, "secret": true}

// setAllowList sets the Allow header for list endpoints.
func (h *Handlers) setAllowList(w http.ResponseWriter, r *http.Request, resourceType string) {
	offer := readOnly
	id := auth.IdentityFromContext(r.Context())
	if h.acl.Can(id, "write", resourceType+":*") ||
		createsByName[resourceType] && grantsWriteOnType(h.acl.PermissionsFor(id), resourceType) {
		offer = h.routedWrites(r, false)
	}
	writeAllow(w, offer)
}

// grantsWriteOnType reports whether a grant pattern names the type itself. A
// stack grant implies the stack's configs and secrets, but a new one belongs
// to no stack, so it cannot permit the create.
func grantsWriteOnType(patterns map[string][]string, resourceType string) bool {
	for pattern, permissions := range patterns {
		if strings.HasPrefix(pattern, resourceType+":") && slices.Contains(permissions, "write") {
			return true
		}
	}

	return false
}
