package acl

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

// stubResolver implements ResourceResolver for testing.
type stubResolver struct {
	stacks   map[string]string // "type:id" -> stack name
	services map[string]string // taskID -> service name
}

func (r *stubResolver) StackOf(resourceType, resourceID string) string {
	return r.stacks[resourceType+":"+resourceID]
}

func (r *stubResolver) ServiceOfTask(taskID string) string {
	return r.services[taskID]
}

func TestEvaluator_NilAllowsAll(t *testing.T) {
	var e *Evaluator
	if !e.Can(nil, "read", "service:foo") {
		t.Fatal("nil evaluator should allow all")
	}
}

func TestEvaluator_NilPolicyAllowsAll(t *testing.T) {
	e := NewEvaluator()
	if !e.Can(nil, "read", "service:foo") {
		t.Fatal("nil policy should allow all")
	}
}

func TestEvaluator_BasicGrant(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
	}})

	alice := &auth.Identity{Subject: "alice"}
	bob := &auth.Identity{Subject: "bob"}

	if !e.Can(alice, "read", "service:webapp") {
		t.Fatal("alice should be able to read service:webapp")
	}
	if e.Can(bob, "read", "service:webapp") {
		t.Fatal("bob should NOT be able to read service:webapp")
	}
	if e.Can(alice, "write", "service:webapp") {
		t.Fatal("alice should NOT be able to write service:webapp (only read grant)")
	}
}

func TestEvaluator_WriteImpliesRead(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"*"}, Audience: []string{"group:ops"}, Permissions: []string{"write"}},
	}})

	ops := &auth.Identity{Subject: "alice", Groups: []string{"ops"}}
	if !e.Can(ops, "read", "service:foo") {
		t.Fatal("write should imply read")
	}
}

func TestEvaluator_GlobMatch(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp-*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	id := &auth.Identity{Subject: "anyone"}
	if !e.Can(id, "read", "service:webapp-api") {
		t.Fatal("should match glob service:webapp-*")
	}
	if e.Can(id, "read", "service:other") {
		t.Fatal("should NOT match service:other")
	}
}

func TestEvaluator_StackResolution(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"stack:monitoring"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})
	e.SetResolver(&stubResolver{
		stacks: map[string]string{"service:svc1": "monitoring"},
	})

	id := &auth.Identity{Subject: "anyone"}
	if !e.Can(id, "read", "service:svc1") {
		t.Fatal("service in monitoring stack should be readable via stack grant")
	}
	if e.Can(id, "read", "service:svc2") {
		t.Fatal("service NOT in monitoring stack should not be readable")
	}
}

func TestEvaluator_TaskInheritance(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})
	e.SetResolver(&stubResolver{
		services: map[string]string{"task123": "webapp"},
	})

	id := &auth.Identity{Subject: "anyone"}
	if !e.Can(id, "read", "task:task123") {
		t.Fatal("task should inherit read from parent service")
	}
}

func TestEvaluator_EmailMatch(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"*"}, Audience: []string{"user:*@example.com"}, Permissions: []string{"read"}},
	}})

	id := &auth.Identity{Subject: "sub-12345", Email: "alice@example.com"}
	if !e.Can(id, "read", "service:foo") {
		t.Fatal("email pattern should match")
	}
}

func TestFilter(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:allow-*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	type svc struct{ name string }
	items := []svc{
		{name: "allow-one"},
		{name: "deny-two"},
		{name: "allow-three"},
	}

	id := &auth.Identity{Subject: "anyone"}
	filtered := Filter(e, id, "read", items, func(s svc) string { return "service:" + s.name })

	if len(filtered) != 2 {
		t.Fatalf("expected 2 items, got %d", len(filtered))
	}
	if filtered[0].name != "allow-one" || filtered[1].name != "allow-three" {
		t.Fatalf("unexpected items: %v", filtered)
	}
}

func TestFilter_NilEvaluator(t *testing.T) {
	type svc struct{ name string }
	items := []svc{{name: "a"}, {name: "b"}}

	filtered := Filter[svc](nil, nil, "read", items, func(s svc) string { return "service:" + s.name })
	if len(filtered) != 2 {
		t.Fatal("nil evaluator should pass all items through")
	}
}

func TestHasAnyGrant(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:foo"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
	}})

	alice := &auth.Identity{Subject: "alice"}
	bob := &auth.Identity{Subject: "bob"}

	if !e.HasAnyGrant(alice) {
		t.Fatal("alice should have at least one grant")
	}
	if e.HasAnyGrant(bob) {
		t.Fatal("bob should NOT have any grants")
	}
}

func TestPermissionsFor(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp-*"}, Audience: []string{"user:alice"}, Permissions: []string{"read", "write"}},
		{Resources: []string{"stack:monitoring"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
	}})

	alice := &auth.Identity{Subject: "alice"}
	perms := e.PermissionsFor(alice)
	if perms == nil {
		t.Fatal("expected non-nil permissions")
	}
	if len(perms["service:webapp-*"]) != 2 {
		t.Fatalf("expected 2 permissions for service:webapp-*, got %v", perms["service:webapp-*"])
	}
	if len(perms["stack:monitoring"]) != 1 {
		t.Fatalf("expected 1 permission for stack:monitoring, got %v", perms["stack:monitoring"])
	}
}

// TestProviderGrants tests that provider grants skip audience checks.
type mockSource struct {
	grants []Grant
}

func (m *mockSource) GrantsFor(_ *auth.Identity) []Grant {
	return m.grants
}

func TestEvaluator_ProviderGrants(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{}}) // Empty file policy
	e.SetSource(&mockSource{
		grants: []Grant{
			{Resources: []string{"service:*"}, Permissions: []string{"read"}},
		},
	})

	id := &auth.Identity{Subject: "anyone"}
	if !e.Can(id, "read", "service:foo") {
		t.Fatal("provider grant should allow read on service:foo")
	}
}
