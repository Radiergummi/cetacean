package acl

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

// stubResolver implements ResourceResolver for testing.
type stubResolver struct {
	stacks   map[string]string            // "type:id" -> stack name
	services map[string]string            // taskID -> service name
	labels   map[string]map[string]string // "type:name" -> labels
}

func (r *stubResolver) StackOf(resourceType, resourceID string) string {
	return r.stacks[resourceType+":"+resourceID]
}

func (r *stubResolver) ServiceOfTask(taskID string) string {
	return r.services[taskID]
}

func (r *stubResolver) LabelsOf(resourceType, name string) map[string]string {
	return r.labels[resourceType+":"+name]
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
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
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
		{
			Resources:   []string{"service:webapp-*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
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
		{
			Resources:   []string{"stack:monitoring"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
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
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
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
		{
			Resources:   []string{"*"},
			Audience:    []string{"user:*@example.com"},
			Permissions: []string{"read"},
		},
	}})

	id := &auth.Identity{Subject: "sub-12345", Email: "alice@example.com"}
	if !e.Can(id, "read", "service:foo") {
		t.Fatal("email pattern should match")
	}
}

func TestFilter(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:allow-*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
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

	filtered := Filter[svc](
		nil,
		nil,
		"read",
		items,
		func(s svc) string { return "service:" + s.name },
	)
	if len(filtered) != 2 {
		t.Fatal("nil evaluator should pass all items through")
	}
}

func TestHasAnyGrant(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:foo"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
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
		{
			Resources:   []string{"service:webapp-*"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read", "write"},
		},
		{
			Resources:   []string{"stack:monitoring"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
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
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
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
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
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

	// Resource with no colon -- direct wildcard match should still work.
	if !e.Can(id, "read", "nocolon") {
		t.Fatal("wildcard * should match even malformed resource")
	}
}

// TestEvaluator_EmptyNameResource verifies that "service:" (empty name) does
// not match any grant — neither concrete nor wildcard. Empty resource names
// are rejected before glob matching is attempted.
func TestEvaluator_EmptyNameResource(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:specific"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	id := &auth.Identity{Subject: "anyone"}
	if e.Can(id, "read", "service:") {
		t.Fatal("empty name should not match service:specific")
	}
}

func TestPermissionsFor_IncludesProviderGrants(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
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
		{
			Resources:   []string{"service:foo"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
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

// Stack grant isolation: service in stack B denied when only stack A granted.
func TestEvaluator_StackGrantIsolation(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"stack:alpha"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})
	e.SetResolver(&stubResolver{
		stacks: map[string]string{
			"service:svc-a": "alpha",
			"service:svc-b": "beta",
		},
	})

	id := &auth.Identity{Subject: "anyone"}
	if !e.Can(id, "read", "service:svc-a") {
		t.Fatal("service in stack alpha should be readable via stack:alpha grant")
	}
	if e.Can(id, "read", "service:svc-b") {
		t.Fatal("service in stack beta should NOT be readable -- only stack:alpha is granted")
	}
}

// Empty but non-nil grants list denies all access (distinct from nil policy).
func TestEvaluator_EmptyGrantsDenyAll(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{}})

	id := &auth.Identity{Subject: "anyone"}
	if e.Can(id, "read", "service:foo") {
		t.Fatal("empty grants policy (non-nil) should deny all access")
	}
	if e.Can(id, "write", "node:bar") {
		t.Fatal("empty grants policy (non-nil) should deny all access")
	}
	if e.HasAnyGrant(id) {
		t.Fatal("empty grants policy should report no grants")
	}
}

// Provider grants with invalid resource types/permissions are inert.
func TestEvaluator_ProviderGrantsWithUnknownPermissions(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetSource(&mockSource{
		grants: []Grant{
			{Resources: []string{"badtype:foo"}, Permissions: []string{"admin"}},
		},
	})

	id := &auth.Identity{Subject: "anyone"}

	// "read" != "admin", so permission check fails.
	if e.Can(id, "read", "badtype:foo") {
		t.Fatal("grant with permission 'admin' should not satisfy 'read' check")
	}

	// "admin" doesn't match "service:foo" because the resource pattern differs.
	if e.Can(id, "admin", "service:foo") {
		t.Fatal("resource mismatch should deny even with matching permission")
	}

	// Exact permission + resource match succeeds: unknown permissions pass
	// through when sourced directly from a provider (not via extractGrantsFromRaw,
	// which validates them).
	if !e.Can(id, "admin", "badtype:foo") {
		t.Fatal("exact permission+resource match should succeed for direct provider grants")
	}
}

// Malformed resources (no colon, empty type, empty name) are always denied.
func TestEvaluator_MalformedResourceAgainstTypedPattern(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
	}})

	id := &auth.Identity{Subject: "anyone"}

	tests := []struct {
		name     string
		resource string
		want     bool
	}{
		{"no colon", "nocolon", false},
		{"empty type", ":noname", false},
		{
			"empty name never matches",
			"service:",
			false,
		}, // empty resource names rejected before glob
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.Can(id, "read", tt.resource)
			if got != tt.want {
				t.Errorf("Can(read, %q) = %v, want %v", tt.resource, got, tt.want)
			}
		})
	}
}

func TestPermissionsFor_Deduplication(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:webapp-*"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"read"},
		},
	}})
	e.SetSource(&mockSource{
		grants: []Grant{
			{Resources: []string{"service:webapp-*"}, Permissions: []string{"read"}},
		},
	})

	alice := &auth.Identity{Subject: "alice"}
	perms := e.PermissionsFor(alice)
	got := perms["service:webapp-*"]
	if len(got) != 1 {
		t.Fatalf("expected deduplicated [read], got %v", got)
	}
	if got[0] != "read" {
		t.Fatalf("expected read, got %s", got[0])
	}
}

func TestEvaluator_TaskInheritsThroughStack(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"stack:monitoring"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})
	e.SetResolver(&stubResolver{
		services: map[string]string{"task-prom-1": "prometheus"},
		stacks:   map[string]string{"service:prometheus": "monitoring"},
	})

	id := &auth.Identity{Subject: "user1"}
	if !e.Can(id, "read", "task:task-prom-1") {
		t.Fatal("task should be readable via task→service→stack chain")
	}
}

func TestEvaluator_CaseSensitiveResourceNames(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:WebApp"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	id := &auth.Identity{Subject: "user1"}
	if !e.Can(id, "read", "service:WebApp") {
		t.Fatal("exact case match should be allowed")
	}
	if e.Can(id, "read", "service:webapp") {
		t.Fatal("different case should not match")
	}
}

func TestEvaluator_CaseSensitiveAudience(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"user:Alice"},
			Permissions: []string{"read"},
		},
	}})

	if !e.Can(&auth.Identity{Subject: "Alice"}, "read", "service:foo") {
		t.Fatal("exact case audience should match")
	}
	if e.Can(&auth.Identity{Subject: "alice"}, "read", "service:foo") {
		t.Fatal("different case audience should not match")
	}
}

func TestEvaluator_OverlappingGrantsUnion(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"group:ops"},
			Permissions: []string{"read"},
		},
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"group:dev"},
			Permissions: []string{"write"},
		},
	}})

	id := &auth.Identity{Subject: "alice", Groups: []string{"ops", "dev"}}
	if !e.Can(id, "read", "service:backend") {
		t.Fatal("ops group should grant read on all services")
	}
	if !e.Can(id, "write", "service:webapp") {
		t.Fatal("dev group should grant write on webapp")
	}
	if e.Can(id, "write", "service:backend") {
		t.Fatal("dev group write should not extend to backend")
	}
}

func TestEvaluator_LabelGrant_ReadOnly(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "group:dev"},
		},
	})
	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	ops := &auth.Identity{Subject: "bob", Groups: []string{"ops"}}
	if !e.Can(dev, "read", "service:webapp") {
		t.Fatal("dev should be able to read via label")
	}
	if e.Can(dev, "write", "service:webapp") {
		t.Fatal("dev should NOT be able to write (label only grants read)")
	}
	if e.Can(ops, "read", "service:webapp") {
		t.Fatal("ops should NOT be able to read (not in label audience)")
	}
}

func TestEvaluator_LabelGrant_WriteImpliesRead(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.write": "group:ops"},
		},
	})
	ops := &auth.Identity{Subject: "alice", Groups: []string{"ops"}}
	if !e.Can(ops, "read", "service:webapp") {
		t.Fatal("write label should imply read")
	}
	if !e.Can(ops, "write", "service:webapp") {
		t.Fatal("ops should be able to write via label")
	}
}

func TestEvaluator_LabelWinsOverConfig(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:*"}, Audience: []string{"group:dev"}, Permissions: []string{"write"}},
	}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "group:*", "cetacean.acl.write": "group:ops"},
		},
	})
	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	ops := &auth.Identity{Subject: "bob", Groups: []string{"ops"}}
	if !e.Can(dev, "read", "service:webapp") {
		t.Fatal("dev should be able to read via label group:*")
	}
	if e.Can(dev, "write", "service:webapp") {
		t.Fatal("dev should NOT be able to write (label narrows config)")
	}
	if !e.Can(ops, "write", "service:webapp") {
		t.Fatal("ops should be able to write via label")
	}
	if !e.Can(dev, "write", "service:other") {
		t.Fatal("dev should still have config write on non-labeled services")
	}
}

func TestEvaluator_LabelSuppressesAllowAll(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:sensitive": {"cetacean.acl.read": "group:ops"},
		},
	})
	ops := &auth.Identity{Subject: "alice", Groups: []string{"ops"}}
	dev := &auth.Identity{Subject: "bob", Groups: []string{"dev"}}
	if !e.Can(ops, "read", "service:sensitive") {
		t.Fatal("ops should be able to read via label")
	}
	if e.Can(dev, "read", "service:sensitive") {
		t.Fatal("dev should be denied (labels suppress default, no config grant)")
	}
}

func TestEvaluator_LabelConfigFillsGaps(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"user:bot"}, Permissions: []string{"write"}},
	}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "group:dev"},
		},
	})
	bot := &auth.Identity{Subject: "bot"}
	if !e.Can(bot, "write", "service:webapp") {
		t.Fatal("bot should have write via explicit config grant")
	}
}

func TestEvaluator_LabelDisabled(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "group:dev"},
		},
	})
	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	if e.Can(dev, "read", "service:webapp") {
		t.Fatal("labels should be ignored when disabled")
	}
}

func TestEvaluator_LabelTaskInheritsFromService(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		services: map[string]string{"task-1": "webapp"},
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "group:dev"},
		},
	})
	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	if !e.Can(dev, "read", "task:task-1") {
		t.Fatal("task should inherit label grants from parent service")
	}
}

func TestEvaluator_LabelAdditiveMostPermissiveWins(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "group:dev", "cetacean.acl.write": "group:dev"},
		},
	})
	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	if !e.Can(dev, "write", "service:webapp") {
		t.Fatal("dev matches both read and write labels, most permissive should win")
	}
}

func TestFilter_WithLabels(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:visible": {"cetacean.acl.read": "group:dev"},
			"service:hidden":  {"cetacean.acl.read": "group:ops"},
		},
	})
	type svc struct{ name string }
	items := []svc{{name: "visible"}, {name: "hidden"}, {name: "unlabeled"}}
	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	filtered := Filter(e, dev, "read", items, func(s svc) string { return "service:" + s.name })
	if len(filtered) != 1 || filtered[0].name != "visible" {
		t.Fatalf("expected [visible], got %v", filtered)
	}
}

func TestEvaluator_NilIdentityWithActivePolicy(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"group:ops"},
			Permissions: []string{"read"},
		},
	}})

	if e.Can(nil, "read", "service:foo") {
		t.Fatal("nil identity should be denied when policy has audience restrictions")
	}
}

func TestEvaluator_UnlabeledResourceWithConfigGrant(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:*"}, Audience: []string{"group:ops"}, Permissions: []string{"read"}},
	}})
	e.SetResolver(&stubResolver{labels: map[string]map[string]string{}})

	ops := &auth.Identity{Subject: "alice", Groups: []string{"ops"}}
	if !e.Can(ops, "read", "service:no-labels") {
		t.Fatal("config grant should reach unlabeled resources when labels enabled")
	}
}

func TestHasAnyGrant_LabelsEnabled_AlwaysTrue(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})

	// Even an identity with zero config/provider grants should return true
	// when labels are enabled — they may have label-based grants on specific resources.
	nobody := &auth.Identity{Subject: "nobody"}
	if !e.HasAnyGrant(nobody) {
		t.Fatal("HasAnyGrant should return true for all identities when labels enabled")
	}
}

// Security edge case tests for label-based ACL

func TestEvaluator_NilIdentityWithLabels(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "group:dev"},
		},
	})
	if e.Can(nil, "read", "service:webapp") {
		t.Fatal("nil identity must be denied even when resource has read labels")
	}
}

func TestEvaluator_LabelWildcardMatchesEmptyIdentity(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "*"},
		},
	})
	empty := &auth.Identity{Subject: "", Email: "", Groups: nil}
	if !e.Can(empty, "read", "service:webapp") {
		t.Fatal("wildcard label audience should match identity with empty fields")
	}
}

func TestEvaluator_LabelCanEscalateAccessBeyondConfigPolicy(t *testing.T) {
	// SECURITY: documents that resource labels CAN grant broader access
	// than config policy. A service operator who can set labels can
	// escalate access on their own resources. This is intentional per
	// the label-as-authoritative design.
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:*"}, Audience: []string{"group:ops"}, Permissions: []string{"read"}},
	}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.write": "*"},
		},
	})
	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	if !e.Can(dev, "write", "service:webapp") {
		t.Fatal("label write:* escalates access beyond config policy — intended behavior")
	}
	if !e.Can(dev, "read", "service:webapp") {
		t.Fatal("label write:* implies read for everyone")
	}
	if e.Can(dev, "read", "service:other") {
		t.Fatal("config policy still applies to unlabeled services")
	}
}

func TestEvaluator_LabelNarrowsWildcardConfigGrant(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"*"}, Audience: []string{"group:dev"}, Permissions: []string{"write"}},
	}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:sensitive": {"cetacean.acl.read": "group:ops"},
		},
	})
	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	// dev not in label audience → falls through to config wildcard grant
	if !e.Can(dev, "write", "service:sensitive") {
		t.Fatal("dev not in label audience → config wildcard grant applies")
	}
	// ops in label audience → label is authoritative: read only
	ops := &auth.Identity{Subject: "bob", Groups: []string{"ops"}}
	if !e.Can(ops, "read", "service:sensitive") {
		t.Fatal("ops should read via label")
	}
	if e.Can(ops, "write", "service:sensitive") {
		t.Fatal("ops label grants only read — label narrows even against wildcard config")
	}
}

func TestEvaluator_ProviderGrantFillsGapForNonLabelAudience(t *testing.T) {
	// SECURITY: when labels are present but identity doesn't match label
	// audience, explicit provider grants can still grant access. This is
	// the "config fills gaps" rule applied to provider grants.
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetSource(&mockSource{
		grants: []Grant{
			{Resources: []string{"service:*"}, Permissions: []string{"write"}},
		},
	})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:sensitive": {"cetacean.acl.read": "group:ops"},
		},
	})
	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	// dev not in label audience → handled=false → provider grant applies
	if !e.Can(dev, "write", "service:sensitive") {
		t.Fatal("provider grant fills gap for non-label audience identity")
	}
}

func TestEvaluator_LabelsEnabledNilResolver(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	// No SetResolver — resolver is nil
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
	}})
	alice := &auth.Identity{Subject: "alice"}
	if !e.Can(alice, "read", "service:webapp") {
		t.Fatal("nil resolver with labels enabled should fall through to config grant without panic")
	}
}

func TestEvaluator_LabelsEnabledNilPolicy(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	// No SetPolicy — policy is nil; labels enabled converts nil to empty
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "group:dev"},
		},
	})
	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	ops := &auth.Identity{Subject: "bob", Groups: []string{"ops"}}
	if !e.Can(dev, "read", "service:webapp") {
		t.Fatal("dev should read via label even with nil policy")
	}
	if e.Can(ops, "read", "service:webapp") {
		t.Fatal("ops not in label audience and nil policy normalized to empty — should deny")
	}
}

func TestEvaluator_LabelMalformedAudienceDoesNotGrant(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "notavalidformat,group:ops"},
		},
	})
	ops := &auth.Identity{Subject: "alice", Groups: []string{"ops"}}
	malformed := &auth.Identity{Subject: "notavalidformat"}
	if !e.Can(ops, "read", "service:webapp") {
		t.Fatal("ops should match the valid group:ops entry")
	}
	if e.Can(malformed, "read", "service:webapp") {
		t.Fatal("malformed audience entry must not accidentally grant access")
	}
}

func TestEvaluator_LabelWhitespaceOnlyFallsThrough(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"user:bot"}, Permissions: []string{"read"}},
	}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {"cetacean.acl.read": "   "},
		},
	})
	bot := &auth.Identity{Subject: "bot"}
	if !e.Can(bot, "read", "service:webapp") {
		t.Fatal("whitespace-only label value should fall through to config grant")
	}
	dev := &auth.Identity{Subject: "dev", Groups: []string{"dev"}}
	if e.Can(dev, "read", "service:webapp") {
		t.Fatal("dev has no config grant and whitespace label grants nothing")
	}
}

func TestEvaluator_LabelTaskParentServiceNoACLLabels(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"service:webapp"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
	}})
	e.SetResolver(&stubResolver{
		services: map[string]string{"task-1": "webapp"},
		labels: map[string]map[string]string{
			"service:webapp": {"com.docker.stack.namespace": "mystack"},
		},
	})
	alice := &auth.Identity{Subject: "alice"}
	if !e.Can(alice, "read", "task:task-1") {
		t.Fatal("task should fall through to config grant when parent service has no ACL labels")
	}
}

func TestEvaluator_LabelUnsupportedResourceType(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"plugin:*"}, Audience: []string{"user:alice"}, Permissions: []string{"read"}},
	}})
	e.SetResolver(&stubResolver{labels: map[string]map[string]string{}})
	alice := &auth.Identity{Subject: "alice"}
	if !e.Can(alice, "read", "plugin:my-plugin") {
		t.Fatal("unsupported label resource type should fall through to config grant")
	}
}
