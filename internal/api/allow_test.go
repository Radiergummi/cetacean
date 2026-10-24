package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"

	"github.com/radiergummi/cetacean/internal/spec"
)

func TestSetAllowList(t *testing.T) {
	h := newTestHandlers(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/nodes", nil)
	h.setAllowList(w, r, "node")
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("setAllowList() = %q, want %q", got, "GET, HEAD")
	}
}

func TestSetAllowList_POST(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{
		Grants: []acl.Grant{
			{
				Resources:   []string{"config:*"},
				Audience:    []string{"*"},
				Permissions: []string{"write"},
			},
		},
	})
	h := &Handlers{operationsLevel: config.OpsConfiguration, acl: e}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/configs", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))

	h.setAllowList(w, r, "config")

	got := w.Header().Get("Allow")
	if got != "GET, HEAD, POST" {
		t.Errorf("setAllowList(config, write grant) = %q, want %q", got, "GET, HEAD, POST")
	}
}

// A new config belongs to no stack, so only a grant naming configs can permit
// creating one; a stack grant implies its configs and nothing outside it.
func TestSetAllowListOffersCreateOnlyForAGrantNamingTheType(t *testing.T) {
	for resource, want := range map[string]string{
		"stack:web":    "GET, HEAD",
		"config:app-*": "GET, HEAD, POST",
		"*":            "GET, HEAD, POST",
	} {
		t.Run(resource, func(t *testing.T) {
			e := acl.NewEvaluator()
			e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
				Resources:   []string{resource},
				Audience:    []string{"*"},
				Permissions: []string{"write"},
			}}})
			h := &Handlers{operationsLevel: config.OpsConfiguration, acl: e}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/configs", nil)
			r = r.WithContext(
				auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}),
			)

			h.setAllowList(w, r, "config")

			if got := w.Header().Get("Allow"); got != want {
				t.Errorf("Allow = %q, want %q", got, want)
			}
		})
	}
}

func TestTheNodeRoleOffersItsPut(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-1"}})
	router := newTestRouterWithCache(t, c)

	req := httptest.NewRequest(http.MethodGet, "/nodes/n1/role", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if got := w.Header().Get("Allow"); got != "GET, HEAD, PUT" {
		t.Errorf("Allow = %q, want GET, HEAD, PUT", got)
	}
	if vary := w.Header().Values("Vary"); !slices.ContainsFunc(vary, func(v string) bool {
		return strings.Contains(v, "Authorization")
	}) {
		t.Errorf("Vary = %q, want it to name the identity", vary)
	}
}

func TestSetAllowList_NoWrite(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{
		Grants: []acl.Grant{
			{
				Resources:   []string{"config:*"},
				Audience:    []string{"*"},
				Permissions: []string{"read"},
			},
		},
	})
	h := &Handlers{operationsLevel: config.OpsConfiguration, acl: e}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/configs", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))

	h.setAllowList(w, r, "config")

	got := w.Header().Get("Allow")
	if got != "GET, HEAD" {
		t.Errorf("setAllowList(config, read only) = %q, want %q", got, "GET, HEAD")
	}
}

func TestSetAllow_NoWriteMethods(t *testing.T) {
	// Resource type with no write methods defined (e.g. a fake type).
	h := newTestHandlers(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/nonexistent/foo", nil)
	h.setAllow(w, r, "nonexistent", "foo")
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("setAllow(nonexistent) = %q, want %q", got, "GET, HEAD")
	}
}

func TestSetAllow_ServiceFullWriteGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:*"}, Audience: []string{"*"}, Permissions: []string{"write"}},
	}})

	h := newTestHandlers(t, withACL(e), withOpsLevel(config.OpsImpactful))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services/webapp", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))
	h.setAllow(w, r, "service", "webapp")

	if got := w.Header().Get("Allow"); got != "GET, HEAD, DELETE" {
		t.Errorf("Allow = %q, want %q", got, "GET, HEAD, DELETE")
	}
	if got := editTargets(w.Header()); !slices.Contains(got, "/services/webapp/scale") ||
		!slices.Contains(got, "/services/webapp/labels") {
		t.Errorf("edit links = %v, want scale and labels among them", got)
	}
}

func TestSetAllow_ServiceReadOnlyGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withACL(e), withOpsLevel(config.OpsImpactful))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services/webapp", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))
	h.setAllow(w, r, "service", "webapp")

	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("setAllow(read-only) = %q, want %q", got, "GET, HEAD")
	}
}

func TestSetAllow_DifferentResourceTypes(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"*"}, Audience: []string{"*"}, Permissions: []string{"write"}},
	}})

	tests := []struct {
		name         string
		resourceType string
		wantMethods  []string
	}{
		{
			name:         "node",
			resourceType: "node",
			wantMethods:  []string{"GET", "HEAD", "DELETE"},
		},
		{
			name:         "task",
			resourceType: "task",
			wantMethods:  []string{"GET", "HEAD", "DELETE"},
		},
		{
			name:         "volume",
			resourceType: "volume",
			wantMethods:  []string{"GET", "HEAD", "DELETE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandlers(t, withACL(e), withOpsLevel(config.OpsImpactful))
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/"+tt.resourceType+"s/test", nil)
			r = r.WithContext(
				auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}),
			)
			h.setAllow(w, r, tt.resourceType, "test")

			allow := w.Header().Get("Allow")
			for _, method := range tt.wantMethods {
				if !strings.Contains(allow, method) {
					t.Errorf(
						"Allow header %q missing method %s for %s",
						allow,
						method,
						tt.resourceType,
					)
				}
			}
		})
	}
}

func TestSetAllow_AcceptPatch_AbsentWhenNoPatch(t *testing.T) {
	h := newTestHandlers(t, withOpsLevel(config.OpsImpactful))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/tasks/abc", nil)
	h.setAllow(w, r, "task", "abc")

	if got := w.Header().Get("Accept-Patch"); got != "" {
		t.Errorf("Accept-Patch should be absent for task, got %q", got)
	}
}

func TestSetAllow_AcceptPatch_AbsentWhenReadOnly(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withACL(e), withOpsLevel(config.OpsImpactful))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services/webapp/labels", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))
	h.setAllowSubResource(w, r, "service:webapp")

	if got := w.Header().Get("Accept-Patch"); got != "" {
		t.Errorf("Accept-Patch should be absent for read-only, got %q", got)
	}
}

func TestSetAllow_AcceptPatch_AbsentWhenLowOpsLevel(t *testing.T) {
	// OpsOperational is below PATCH tier (OpsConfiguration) for services.
	h := newTestHandlers(t, withOpsLevel(config.OpsOperational))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services/webapp/labels", nil)
	h.setAllowSubResource(w, r, "service:webapp")

	if got := w.Header().Get("Accept-Patch"); got != "" {
		t.Errorf("Accept-Patch should be absent at OpsOperational, got %q", got)
	}
}

func TestSetAllow_NilACL(t *testing.T) {
	// Nil ACL evaluator = allow all.
	h := newTestHandlers(t, withOpsLevel(config.OpsImpactful))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services/webapp", nil)
	h.setAllow(w, r, "service", "webapp")

	// With nil ACL, Can() returns true, so every method the level reaches is offered.
	if got := w.Header().Get("Allow"); got != "GET, HEAD, DELETE" {
		t.Errorf("Allow = %q with nil ACL, want %q", got, "GET, HEAD, DELETE")
	}
}

// The node labels editor opens at operations level 2, the level its route
// takes. The tier is asserted rather than read from the routes, which would
// pass whatever they said.
func TestNodeLabelsAreLinkedAtTierTwo(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"node:*"}, Audience: []string{"*"}, Permissions: []string{"write"}},
	}})

	h := newTestHandlers(t, withACL(e), withOpsLevel(config.OpsConfiguration))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/nodes/node1", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))
	h.setAllow(w, r, "node", "node1")

	if got := editTargets(w.Header()); !slices.Contains(got, "/nodes/node1/labels") {
		t.Errorf("edit links = %v at operations level 2, want the labels", got)
	}
}

// assertVariesByIdentity fails unless the response is marked as per-caller.
// Nothing else tells an intermediary that these bodies differ between callers.
func assertVariesByIdentity(t *testing.T, what string, h http.Header) {
	t.Helper()

	vary := strings.Join(h.Values("Vary"), ", ")
	if !strings.Contains(vary, "Authorization, Cookie") {
		t.Errorf("%s carried Vary %q, want it to include %q",
			what, vary, "Authorization, Cookie")
	}
}

// A response reporting a per-identity Allow is per-identity, so whatever
// computes one says so too.
func TestAllowSeamVariesByIdentity(t *testing.T) {
	h := newTestHandlers(t)
	r := httptest.NewRequest("GET", "/nodes", nil)

	seams := map[string]func(http.ResponseWriter){
		"setAllowList": func(w http.ResponseWriter) { h.setAllowList(w, r, "node") },
		"setAllow":     func(w http.ResponseWriter) { h.setAllow(w, r, "node", "node-1") },
		"setAllowSubResource": func(w http.ResponseWriter) {
			h.setAllowSubResource(w, r, "node:node-1")
		},
	}

	for name, seam := range seams {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			seam(w)

			if values := w.Header().Values("Vary"); len(values) != 1 {
				t.Fatalf("%s set %d Vary headers, want 1: %q", name, len(values), values)
			}
			assertVariesByIdentity(t, name, w.Header())
		})
	}
}

// A 304 is as identity-dependent as the 200 it stands in for, and reaches the
// seam by the same route.
func TestNotModifiedVariesByIdentity(t *testing.T) {
	h := newTestHandlers(t, withCache(validatorCache(5)))

	full := listOnce(t, h, "/api/nodes", nil, "")
	notModified := listOnce(t, h, "/api/nodes", nil, full.Header().Get("ETag"))
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("revalidation returned %d, want 304", notModified.Code)
	}

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"200": full,
		"304": notModified,
	} {
		assertVariesByIdentity(t, name, rec.Header())
	}
}

// Feeds have no Allow to pair with, so each renderer says it itself — and
// neither format may be the one that forgets.
func TestFeedRenderersVaryByIdentity(t *testing.T) {
	renderers := map[string]feedRenderer{"atom": renderAtom, "json": renderJSONFeed}

	for name, render := range renderers {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			render(w, httptest.NewRequest("GET", "/api/recommendations", nil), feedData{})

			assertVariesByIdentity(t, name+" feed", w.Header())
		})
	}
}

// Topology, search, history, recommendations and the stack summary are all
// ACL-filtered and none reports an Allow to carry the marker for them. The
// grant gate they share does.
func TestGrantGatedReadsVaryByIdentity(t *testing.T) {
	h := newTestHandlers(t, withCache(validatorCache(5)))

	reads := map[string]func(http.ResponseWriter, *http.Request){
		"topology": h.HandleTopology,
		"search":   h.HandleSearch,
		"history":  h.HandleHistory,
	}

	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			read(w, httptest.NewRequest("GET", "/api/"+name+"?q=x", nil))

			assertVariesByIdentity(t, name, w.Header())
		})
	}
}

// Two seams writing the marker must not write it twice.
func TestVaryByIdentityStatedOnce(t *testing.T) {
	w := httptest.NewRecorder()
	varyByIdentity(w)
	varyByIdentity(w)

	if got := w.Header().Values("Vary"); len(got) != 1 {
		t.Errorf("Vary = %q, want a single value", got)
	}
}

// Every method Allow offers on a resource must be routed at that path; POST
// creates a config or secret on the collection, never on one of them.
func TestAllowOffersNoMethodWithoutARoute(t *testing.T) {
	h := newTestHandlers(t, withOpsLevel(config.OpsImpactful))

	for _, tc := range []struct{ path, kind string }{
		{"/secrets/s1", "secret"},
		{"/configs/c1", "config"},
	} {
		w := httptest.NewRecorder()
		h.setAllow(w, httptest.NewRequest("GET", tc.path, nil), tc.kind, "x")

		if got := w.Header().Get("Allow"); got != "GET, HEAD, DELETE" {
			t.Errorf("Allow on %s = %q, want %q", tc.path, got, "GET, HEAD, DELETE")
		}
	}
}

// Installing a plugin takes operations level 3, so level 2 must not offer it.
func TestAllowOnPluginsOffersInstallOnlyAtTierThree(t *testing.T) {
	for level, want := range map[config.OperationsLevel]string{
		config.OpsConfiguration: "GET, HEAD",
		config.OpsImpactful:     "GET, HEAD, POST",
	} {
		h := newTestHandlers(t, withOpsLevel(level))
		w := httptest.NewRecorder()
		h.setAllowList(w, httptest.NewRequest("GET", "/plugins", nil), "plugin")

		if got := w.Header().Get("Allow"); got != want {
			t.Errorf("Allow on /plugins at level %d = %q, want %q", level, got, want)
		}
	}
}

// A write route registered without a tiered chain would never reach Allow, and
// the table Allow reads must be the one a configured router serves.
func TestEveryWriteRouteReachesAllow(t *testing.T) {
	_, routes := newRouter(testRouterConfig(t, nil))

	if !slices.Equal(routes.tiered, routeTable().tiered) {
		t.Errorf("a configured router's write routes differ from the table Allow reads")
	}

	for _, pattern := range routes.patterns {
		method, path, _ := strings.Cut(pattern, " ")
		if !slices.Contains(writeMethods, method) || pattern == "POST /-/resync" {
			continue
		}

		if !slices.ContainsFunc(routes.tiered, func(route tieredRoute) bool {
			return route.method == method && route.path == path
		}) {
			t.Errorf("%s is not registered through a tiered chain", pattern)
		}
	}
}

// Allow is computed on the name the write gate checks, or it offers what the
// gate refuses, or hides what it permits.
func TestDetailAllowUsesTheWriteGatesName(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"}) // no hostname reported
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1"})

	allow := func(t *testing.T, path string, grants ...string) string {
		t.Helper()

		e := acl.NewEvaluator()
		e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
			Resources: grants, Audience: []string{"*"}, Permissions: []string{"write"},
		}}})
		e.SetResolver(c)
		router := newTestRouterWithCache(t, c, withACL(e), withOpsLevel(config.OpsImpactful))

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		return rec.Header().Get("Allow")
	}

	if got := allow(t, "/nodes/node1", "node:node1"); !strings.Contains(got, "DELETE") {
		t.Errorf("a hostname-less node writable by ID: Allow = %q, want DELETE", got)
	}
	if got := allow(t, "/tasks/task1", "task:*"); strings.Contains(got, "DELETE") {
		t.Errorf("a task grant the DELETE gate does not read: Allow = %q, want no DELETE", got)
	}
	if got := allow(t, "/tasks/task1", "service:web"); !strings.Contains(got, "DELETE") {
		t.Errorf("write on the task's service: Allow = %q, want DELETE", got)
	}
}

// editTargets lists the rel="edit" targets a response carries.
func editTargets(h http.Header) []string {
	var targets []string
	for _, value := range h.Values("Link") {
		for link := range strings.SplitSeq(value, ", ") {
			target, params, _ := strings.Cut(link, ">;")
			if strings.TrimSpace(params) == `rel="edit"` {
				targets = append(targets, strings.TrimPrefix(target, "<"))
			}
		}
	}
	return targets
}

// Allow names the target's own methods; what may be written beneath it is
// linked instead, one sub-resource per link.
func TestDetailAllowIsStrictAndLinksWritableSubResources(t *testing.T) {
	for _, tc := range []struct {
		level     config.OperationsLevel
		wantAllow string
		wantEdit  []string
	}{
		{config.OpsOperational, "GET, HEAD", []string{
			"/services/web/image", "/services/web/restart", "/services/web/rollback",
			"/services/web/scale",
		}},
		{config.OpsImpactful, "GET, HEAD, DELETE", nil},
	} {
		h := newTestHandlers(t, withOpsLevel(tc.level))
		w := httptest.NewRecorder()
		h.setAllow(w, httptest.NewRequest("GET", "/services/web", nil), "service", "web")

		if got := w.Header().Get("Allow"); got != tc.wantAllow {
			t.Errorf("level %d: Allow = %q, want %q", tc.level, got, tc.wantAllow)
		}
		got := editTargets(w.Header())
		slices.Sort(got)
		if tc.wantEdit != nil && !slices.Equal(got, tc.wantEdit) {
			t.Errorf("level %d: edit links = %v, want %v", tc.level, got, tc.wantEdit)
		}
		if tc.level == config.OpsImpactful && !slices.Contains(got, "/services/web/endpoint-mode") {
			t.Errorf("level 3: edit links = %v, want endpoint-mode among them", got)
		}
	}
}

// At level 2 the dispatcher is writable and the CA is not, which the union of
// PATCH could not tell apart.
func TestSwarmLinksOnlyTheSettingsTheLevelReaches(t *testing.T) {
	h := newTestHandlers(t, withOpsLevel(config.OpsConfiguration))
	w := httptest.NewRecorder()
	h.setAllow(w, httptest.NewRequest("GET", "/swarm", nil), "swarm", "cluster")

	got := editTargets(w.Header())
	if !slices.Contains(got, "/swarm/dispatcher") || slices.Contains(got, "/swarm/ca") {
		t.Errorf("edit links at level 2 = %v, want dispatcher and not ca", got)
	}
}

func TestDetailLinksNothingWithoutAWriteGrant(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})
	h := newTestHandlers(t, withACL(e), withOpsLevel(config.OpsImpactful))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services/web", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))
	h.setAllow(w, r, "service", "web")

	if got := editTargets(w.Header()); len(got) != 0 {
		t.Errorf("edit links for a read-only caller = %v, want none", got)
	}
}

// Accept-Patch belongs to the resource that takes the PATCH, with the formats
// that one parses.
func TestAcceptPatchIsAdvertisedWhereThePatchGoes(t *testing.T) {
	spec.Satisfies(t, "http/rfc5789/accept-patch-is-advertised")

	h := newTestHandlers(t, withOpsLevel(config.OpsImpactful))
	for path, want := range map[string]string{
		"/services/web":           "",
		"/services/web/labels":    AcceptPatch,
		"/services/web/env":       AcceptPatch,
		"/services/web/resources": AcceptMergePatch,
		"/services/web/mode":      "",
		"/nodes/n1/labels":        AcceptPatch,
		"/configs/c1/labels":      AcceptPatch,
		"/secrets/s1/labels":      AcceptPatch,
		"/services/web/ports":     AcceptMergePatch,
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		if path == "/services/web" {
			h.setAllow(w, r, "service", "web")
		} else {
			h.setAllowSubResource(w, r, "service:web")
		}

		if got := w.Header().Get("Accept-Patch"); got != want {
			t.Errorf("Accept-Patch on %s = %q, want %q", path, got, want)
		}
	}
}

// A patchable sub-resource reports its own Allow and the patch formats it takes.
func TestPatchableSubResourcesReportTheirOwnAllow(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	router := newTestRouterWithCache(t, c, withOpsLevel(config.OpsImpactful))

	for path, want := range map[string]string{
		"/services/svc1/labels":    AcceptPatch,
		"/services/svc1/env":       AcceptPatch,
		"/services/svc1/resources": AcceptMergePatch,
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if got := rec.Header().Get("Allow"); got != "GET, HEAD, PATCH" {
			t.Errorf("Allow on %s = %q, want %q", path, got, "GET, HEAD, PATCH")
		}
		if got := rec.Header().Get("Accept-Patch"); got != want {
			t.Errorf("Accept-Patch on %s = %q, want %q", path, got, want)
		}
	}
}

// A plugin's name may hold a slash, which its links keep escaped: decoded, the
// target would name a path no route serves.
func TestEditLinksKeepAnEscapedName(t *testing.T) {
	h := newTestHandlers(t, withOpsLevel(config.OpsImpactful))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/plugins/vieux%2Fsshfs%3Alatest", nil)
	h.setAllow(w, r, "plugin", "vieux/sshfs:latest")

	got := editTargets(w.Header())
	if !slices.Contains(got, "/plugins/vieux%2Fsshfs%3Alatest/enable") {
		t.Errorf("edit links = %v, want the name kept escaped", got)
	}
}
