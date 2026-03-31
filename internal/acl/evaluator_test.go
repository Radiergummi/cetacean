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

func TestEvaluator_EmptyAudienceMatchesEveryone(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:*"}, Audience: nil, Permissions: []string{"read"}},
	}})

	alice := &auth.Identity{Subject: "alice"}
	bob := &auth.Identity{Subject: "bob"}

	if !e.Can(alice, "read", "service:foo") {
		t.Fatal("empty audience should match alice")
	}
	if !e.Can(bob, "read", "service:foo") {
		t.Fatal("empty audience should match bob")
	}
	// Even nil identity should match empty audience.
	if !e.Can(nil, "read", "service:foo") {
		t.Fatal("empty audience should match nil identity")
	}
}

func TestEvaluator_OverlappingFileAndProviderGrants(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
	}})
	e.SetSource(&mockSource{
		grants: []Grant{
			{Resources: []string{"node:*"}, Permissions: []string{"write"}},
		},
	})

	alice := &auth.Identity{Subject: "alice"}

	// File grant: read service:webapp
	if !e.Can(alice, "read", "service:webapp") {
		t.Fatal("alice should be able to read service:webapp via file grant")
	}
	// Provider grant: write node:*
	if !e.Can(alice, "write", "node:orb-1") {
		t.Fatal("alice should be able to write node:orb-1 via provider grant")
	}
	// Neither grant covers write on service:webapp.
	if e.Can(alice, "write", "service:webapp") {
		t.Fatal("alice should NOT be able to write service:webapp")
	}
}

func TestEvaluator_TaskWithNoParentServiceAndNoStack(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})
	e.SetResolver(&stubResolver{
		services: map[string]string{}, // no task->service mapping
		stacks:   map[string]string{}, // no stack membership
	})

	id := &auth.Identity{Subject: "anyone"}
	if e.Can(id, "read", "task:orphan-task") {
		t.Fatal("orphan task (no parent service, no stack) should be denied")
	}
}

func TestEvaluator_MalformedResource(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	id := &auth.Identity{Subject: "anyone"}

	// Resource with no colon — direct wildcard match should still work.
	if !e.Can(id, "read", "nocolon") {
		t.Fatal("wildcard * should match even malformed resource")
	}
}

func TestEvaluator_EmptyNameResource(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:specific"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	id := &auth.Identity{Subject: "anyone"}
	if e.Can(id, "read", "service:") {
		t.Fatal("empty name should not match service:specific")
	}
}

func TestPermissionsFor_IncludesProviderGrants(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
	}})
	e.SetSource(&mockSource{
		grants: []Grant{
			{Resources: []string{"node:*"}, Permissions: []string{"write"}},
		},
	})

	alice := &auth.Identity{Subject: "alice"}
	perms := e.PermissionsFor(alice)
	if perms == nil {
		t.Fatal("expected non-nil permissions")
	}
	if len(perms["service:webapp"]) != 1 || perms["service:webapp"][0] != "read" {
		t.Errorf("expected [read] for service:webapp, got %v", perms["service:webapp"])
	}
	if len(perms["node:*"]) != 1 || perms["node:*"][0] != "write" {
		t.Errorf("expected [write] for node:*, got %v", perms["node:*"])
	}
}

func TestHasAnyGrant_ProviderOnlyGrants(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		// File grant only for alice.
		{Resources: []string{"service:foo"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
	}})
	e.SetSource(&mockSource{
		grants: []Grant{
			{Resources: []string{"node:*"}, Permissions: []string{"read"}},
		},
	})

	// Bob has no file grants, but provider grants exist.
	bob := &auth.Identity{Subject: "bob"}
	if !e.HasAnyGrant(bob) {
		t.Fatal("bob should have grants via provider source")
	}
}

func TestEvaluator_SetOnNil(t *testing.T) {
	var e *Evaluator
	// These should not panic.
	e.SetPolicy(&Policy{})
	e.SetResolver(&stubResolver{})
	e.SetSource(&mockSource{})
}
