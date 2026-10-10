package api

import (
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"

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
	// bob has no grants, but sync events carry no resource data and must
	// always pass so clients refetch via ACL-filtered JSON endpoints.
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

// A removed task is gone from the cache the evaluator resolves it through, so
// a service-scoped grant must still reach its remove on that service's stream.
func TestAclMatchWrap_RemovedTaskInheritsServiceGrant(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "other"}},
	})

	e := acl.NewEvaluator()
	e.SetResolver(c)
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	h := newTestHandlers(t, withCache(c), withACL(e))
	r := httptest.NewRequest("GET", "/services/svc1", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))
	matcher := h.aclMatchWrap(r, nil)

	removed := map[string]cache.Event{}
	c.AddOnChangeListener(func(ev cache.Event) {
		if ev.Action == "remove" {
			removed[ev.ID] = ev
		}
	})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1"})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc2"})
	c.DeleteTask("t1")
	c.DeleteTask("t2")

	if !matcher(removed["t1"]) {
		t.Error("a task removed from a readable service should pass")
	}
	if matcher(removed["t2"]) {
		t.Error("a task removed from an unreadable service should not pass")
	}
}
