package acl

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

type stubSource []Grant

func (s stubSource) GrantsFor(*auth.Identity) []Grant { return s }

func TestExecIsNeverImplicit(t *testing.T) {
	alice := &auth.Identity{Subject: "alice"}

	var nilEvaluator *Evaluator
	if nilEvaluator.Can(alice, PermissionExec, "service:web") {
		t.Error("a nil evaluator granted exec")
	}
	if !nilEvaluator.Can(alice, "write", "service:web") {
		t.Error("a nil evaluator stopped granting write")
	}

	noPolicy := NewEvaluator()
	if noPolicy.Can(alice, PermissionExec, "service:web") {
		t.Error("an evaluator without a policy granted exec")
	}
	if !noPolicy.TypeGrants(alice).Can("read", "service") {
		t.Error("allow-all stopped covering read")
	}
	if noPolicy.TypeGrants(alice).Can(PermissionExec, "service") {
		t.Error("allow-all covered exec")
	}
}

func TestWriteDoesNotImplyExec(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{Resources: []string{"*"}, Audience: []string{"*"}, Permissions: []string{"read", "write"}},
	}})

	if e.Can(&auth.Identity{Subject: "alice"}, PermissionExec, "service:web") {
		t.Error("write granted exec")
	}
}

func TestExecImpliesRead(t *testing.T) {
	e := NewEvaluator()
	e.SetPolicy(&Policy{Grants: []Grant{
		{
			Resources:   []string{"service:web"},
			Audience:    []string{"*"},
			Permissions: []string{"exec"},
		},
	}})
	alice := &auth.Identity{Subject: "alice"}

	if !e.Can(alice, PermissionExec, "service:web") {
		t.Error("an exec grant did not grant exec")
	}
	if !e.Can(alice, "read", "service:web") {
		t.Error("exec did not imply read")
	}
	if e.Can(alice, "write", "service:web") {
		t.Error("exec implied write")
	}
}

func TestExecFromProviderWithoutPolicy(t *testing.T) {
	e := NewEvaluator()
	e.SetSource(stubSource{{Resources: []string{"service:web"}, Permissions: []string{"exec"}}})

	if !e.Can(&auth.Identity{Subject: "alice"}, PermissionExec, "service:web") {
		t.Error("a provider exec grant was ignored without a file policy")
	}
}

func TestFilterWithoutPolicyDropsExec(t *testing.T) {
	items := []string{"a", "b"}
	got := Filter(NewEvaluator(), &auth.Identity{Subject: "alice"}, PermissionExec, items,
		func(s string) string { return "service:" + s })
	if len(got) != 0 {
		t.Errorf("Filter granted exec without a policy: %v", got)
	}
}

func TestPolicyAcceptsExec(t *testing.T) {
	p, err := ParsePolicy([]byte(`grants:
  - resources: ["service:web"]
    audience: ["group:ops"]
    permissions: ["exec"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := Validate(p); err != nil {
		t.Errorf("exec was rejected: %v", err)
	}
}
