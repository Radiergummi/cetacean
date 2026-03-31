package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

func TestSetAllowList(t *testing.T) {
	h := newTestHandlers(t)
	w := httptest.NewRecorder()
	h.setAllowList(w)
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("setAllowList() = %q, want %q", got, "GET, HEAD")
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
			r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))
			h.setAllow(w, r, tt.resourceType, "test")

			allow := w.Header().Get("Allow")
			for _, method := range tt.wantMethods {
				if !strings.Contains(allow, method) {
					t.Errorf("Allow header %q missing method %s for %s", allow, method, tt.resourceType)
				}
			}
		})
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
