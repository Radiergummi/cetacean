package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
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

	allow := w.Header().Get("Allow")
	for _, method := range []string{"GET", "HEAD", "PUT", "POST", "PATCH", "DELETE"} {
		if !strings.Contains(allow, method) {
			t.Errorf("Allow header %q missing method %s", allow, method)
		}
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

func TestSetAllow_ServiceLowOpsLevel(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:*"}, Audience: []string{"*"}, Permissions: []string{"write"}},
	}})

	// OpsOperational: only tier1 methods (PUT, POST for service).
	h := newTestHandlers(t, withACL(e), withOpsLevel(config.OpsOperational))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services/webapp", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))
	h.setAllow(w, r, "service", "webapp")

	allow := w.Header().Get("Allow")
	// Should include PUT and POST (tier1) but not PATCH (tier2) or DELETE (tier3).
	if !strings.Contains(allow, "PUT") {
		t.Errorf("Allow header %q should include PUT at OpsOperational", allow)
	}
	if !strings.Contains(allow, "POST") {
		t.Errorf("Allow header %q should include POST at OpsOperational", allow)
	}
	if strings.Contains(allow, "PATCH") {
		t.Errorf("Allow header %q should NOT include PATCH at OpsOperational", allow)
	}
	if strings.Contains(allow, "DELETE") {
		t.Errorf("Allow header %q should NOT include DELETE at OpsOperational", allow)
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
			wantMethods:  []string{"GET", "HEAD", "PUT", "PATCH", "DELETE"},
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

func TestSetAllow_AcceptPatch_ServiceFullWrite(t *testing.T) {
	h := newTestHandlers(t, withOpsLevel(config.OpsImpactful))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services/webapp", nil)
	h.setAllow(w, r, "service", "webapp")

	got := w.Header().Get("Accept-Patch")
	if got != AcceptPatch {
		t.Errorf("Accept-Patch = %q, want %q", got, AcceptPatch)
	}
}

func TestSetAllow_AcceptPatch_SwarmMergeOnly(t *testing.T) {
	h := newTestHandlers(t, withOpsLevel(config.OpsImpactful))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/swarm", nil)
	h.setAllow(w, r, "swarm", "swarm")

	got := w.Header().Get("Accept-Patch")
	if got != AcceptMergePatch {
		t.Errorf("Accept-Patch = %q, want %q", got, AcceptMergePatch)
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
	r := httptest.NewRequest("GET", "/services/webapp", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))
	h.setAllow(w, r, "service", "webapp")

	if got := w.Header().Get("Accept-Patch"); got != "" {
		t.Errorf("Accept-Patch should be absent for read-only, got %q", got)
	}
}

func TestSetAllow_AcceptPatch_AbsentWhenLowOpsLevel(t *testing.T) {
	// OpsOperational is below PATCH tier (OpsConfiguration) for services.
	h := newTestHandlers(t, withOpsLevel(config.OpsOperational))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services/webapp", nil)
	h.setAllow(w, r, "service", "webapp")

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

	allow := w.Header().Get("Allow")
	// With nil ACL, Can() returns true, so all methods at/below ops level are included.
	for _, method := range []string{"GET", "HEAD", "PUT", "POST", "PATCH", "DELETE"} {
		if !strings.Contains(allow, method) {
			t.Errorf("Allow header %q missing method %s with nil ACL", allow, method)
		}
	}
}

// TestAllowHeaderOffersNodePatchAtTierTwo fails while allow.go still calls a
// node PATCH impactful, which hides the dashboard's labels editor on an API
// that accepts the edit. TestEveryOperationIsGatedAtItsDeclaredTier holds the
// route's own gate to the same tier but reads no Allow header, and
// resourceWriteMethods is a second, hand-stated projection of it.
//
// The tier is asserted rather than read from that table: a walk deriving its
// expectation from resourceWriteMethods would pass whatever the table said.
func TestAllowHeaderOffersNodePatchAtTierTwo(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"node:*"}, Audience: []string{"*"}, Permissions: []string{"write"}},
	}})

	h := newTestHandlers(t, withACL(e), withOpsLevel(config.OpsConfiguration))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/nodes/node1", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))
	h.setAllow(w, r, "node", "node1")

	if allow := w.Header().Get("Allow"); !strings.Contains(allow, "PATCH") {
		t.Errorf("Allow = %q at operations level 2, want PATCH offered", allow)
	}
}

// Every response carrying a per-identity Allow is a per-identity response, so
// the two are set together. Without Vary an intermediary has nothing telling it
// these bodies differ per caller; Cache-Control: no-cache keeps it from serving
// one wrongly, but revalidation is cheap now and actually happens.
func TestAllowSeamVariesByIdentity(t *testing.T) {
	h := newTestHandlers(t)
	r := httptest.NewRequest("GET", "/nodes", nil)

	seams := map[string]func(w *httptest.ResponseRecorder){
		"setAllowList": func(w *httptest.ResponseRecorder) {
			h.setAllowList(w, r, "node")
		},
		"setAllow": func(w *httptest.ResponseRecorder) {
			h.setAllow(w, r, "node", "node-1")
		},
		"setAllowSubResource": func(w *httptest.ResponseRecorder) {
			h.setAllowSubResource(w, r, "PUT", config.OpsOperational, "node:node-1")
		},
	}

	for name, seam := range seams {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			seam(w)

			values := w.Header().Values("Vary")
			if len(values) != 1 {
				t.Fatalf("%s set %d Vary headers, want 1: %q", name, len(values), values)
			}
			if got := values[0]; got != "Authorization, Cookie" {
				t.Errorf("%s set Vary %q, want %q", name, got, "Authorization, Cookie")
			}
		})
	}
}

// A 304 is as identity-dependent as the 200 it stands in for, and it travels
// the same seam to say so.
func TestNotModifiedVariesByIdentity(t *testing.T) {
	h := newTestHandlers(t, withCache(validatorCache(5)))

	full := listOnce(t, h, "/api/nodes", nil, "")
	notModified := listOnce(t, h, "/api/nodes", nil, full.Header().Get("ETag"))
	if notModified.Code != 304 {
		t.Fatalf("revalidation returned %d, want 304", notModified.Code)
	}

	for _, rec := range []struct {
		name string
		vary string
	}{
		{"200", strings.Join(full.Header().Values("Vary"), ", ")},
		{"304", strings.Join(notModified.Header().Values("Vary"), ", ")},
	} {
		for _, want := range []string{"Authorization, Cookie", "Accept-Encoding"} {
			if !strings.Contains(rec.vary, want) {
				t.Errorf("%s carried Vary %q, want it to include %q", rec.name, rec.vary, want)
			}
		}
	}
}

// Feeds do not travel the allow seam — they have no Allow header to pair with —
// so each renderer says it itself. Both formats are ACL-filtered and neither may
// be the one that forgets.
func TestFeedRenderersVaryByIdentity(t *testing.T) {
	renderers := map[string]feedRenderer{
		"atom": renderAtom,
		"json": renderJSONFeed,
	}

	for name, render := range renderers {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/recommendations", nil)

			render(w, r, feedData{Title: "Recommendations", Updated: time.Now()})

			vary := strings.Join(w.Header().Values("Vary"), ", ")
			if !strings.Contains(vary, "Authorization, Cookie") {
				t.Errorf("%s feed carried Vary %q, want it to include %q",
					name, vary, "Authorization, Cookie")
			}
		})
	}
}
