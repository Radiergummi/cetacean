package api

import (
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestAclMatchWrap_ReadableEvent(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"service:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	h := newTestHandlers(t, withACL(e))
	r := httptest.NewRequest("GET", "/services", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))

	matcher := h.aclMatchWrap(r, nil)
	ev := cache.Event{Type: cache.EventService, Name: "webapp"}
	if !matcher(ev) {
		t.Fatal("readable service event should pass through")
	}
}

func TestAclMatchWrap_UnreadableEvent(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withACL(e))
	r := httptest.NewRequest("GET", "/services", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "bob"}))

	matcher := h.aclMatchWrap(r, nil)
	ev := cache.Event{Type: cache.EventService, Name: "webapp"}
	if matcher(ev) {
		t.Fatal("bob should NOT be able to see service:webapp")
	}
}

func TestAclMatchWrap_SyncEvent(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withACL(e))
	r := httptest.NewRequest("GET", "/services", nil)
	// bob has no grants, but sync events should always pass.
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "bob"}))

	matcher := h.aclMatchWrap(r, nil)
	ev := cache.Event{Type: cache.EventSync}
	if !matcher(ev) {
		t.Fatal("sync events should always pass through regardless of ACL")
	}
}

func TestAclMatchWrap_InnerMatcherRejects(t *testing.T) {
	// No ACL restrictions (nil evaluator = allow all).
	h := newTestHandlers(t)
	r := httptest.NewRequest("GET", "/services", nil)

	inner := func(ev cache.Event) bool {
		return ev.Name == "allowed"
	}
	matcher := h.aclMatchWrap(r, inner)

	if matcher(cache.Event{Type: cache.EventService, Name: "blocked"}) {
		t.Fatal("inner matcher should reject event")
	}
	if !matcher(cache.Event{Type: cache.EventService, Name: "allowed"}) {
		t.Fatal("inner matcher should allow event")
	}
}

func TestAclMatchWrap_StackEventFiltered(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"stack:webapp"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withACL(e))
	r := httptest.NewRequest("GET", "/events", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))

	matcher := h.aclMatchWrap(r, nil)

	// stack:monitoring should be blocked -- user only has stack:webapp grant.
	if matcher(cache.Event{Type: cache.EventStack, Name: "monitoring"}) {
		t.Fatal("stack:monitoring event should be blocked")
	}

	// stack:webapp should pass through.
	if !matcher(cache.Event{Type: cache.EventStack, Name: "webapp"}) {
		t.Fatal("stack:webapp event should pass through")
	}
}

func TestAclMatchWrap_NilInnerMatcher(t *testing.T) {
	// Nil inner matcher + nil evaluator = all events pass.
	h := newTestHandlers(t)
	r := httptest.NewRequest("GET", "/services", nil)

	matcher := h.aclMatchWrap(r, nil)
	if !matcher(cache.Event{Type: cache.EventService, Name: "anything"}) {
		t.Fatal("nil inner + nil ACL should pass all events")
	}
}
