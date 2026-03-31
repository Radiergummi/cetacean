package acl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePolicy_JSON(t *testing.T) {
	data := []byte(`{"grants":[{"resources":["*"],"audience":["group:ops"],"permissions":["read","write"]}]}`)
	p, err := ParsePolicy(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(p.Grants))
	}
	if p.Grants[0].Resources[0] != "*" {
		t.Fatalf("expected resource *, got %s", p.Grants[0].Resources[0])
	}
}

func TestParsePolicy_YAML(t *testing.T) {
	data := []byte("grants:\n  - resources: [\"*\"]\n    audience: [\"group:ops\"]\n    permissions: [\"read\"]\n")
	p, err := ParsePolicy(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(p.Grants))
	}
}

func TestParsePolicy_TOML(t *testing.T) {
	data := []byte("[[grants]]\nresources = [\"*\"]\naudience = [\"group:ops\"]\npermissions = [\"read\"]\n")
	p, err := ParsePolicy(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(p.Grants))
	}
}

func TestParsePolicyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(`{"grants":[{"resources":["service:*"],"audience":["user:alice"],"permissions":["read"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := ParsePolicyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(p.Grants))
	}
}

func TestValidate_Valid(t *testing.T) {
	p := &Policy{Grants: []Grant{
		{Resources: []string{"*"}, Audience: []string{"group:ops"}, Permissions: []string{"read", "write"}},
		{Resources: []string{"service:webapp-*"}, Audience: []string{"user:alice@example.com"}, Permissions: []string{"read"}},
	}}
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
}

func TestValidate_InvalidResourceType(t *testing.T) {
	p := &Policy{Grants: []Grant{
		{Resources: []string{"foo:bar"}, Permissions: []string{"read"}},
	}}
	if err := Validate(p); err == nil {
		t.Fatal("expected error for invalid resource type")
	}
}

func TestValidate_InvalidAudienceKind(t *testing.T) {
	p := &Policy{Grants: []Grant{
		{Resources: []string{"*"}, Audience: []string{"role:admin"}, Permissions: []string{"read"}},
	}}
	if err := Validate(p); err == nil {
		t.Fatal("expected error for invalid audience kind")
	}
}

func TestValidate_InvalidPermission(t *testing.T) {
	p := &Policy{Grants: []Grant{
		{Resources: []string{"*"}, Permissions: []string{"execute"}},
	}}
	if err := Validate(p); err == nil {
		t.Fatal("expected error for invalid permission")
	}
}

func TestValidate_EmptyResources(t *testing.T) {
	p := &Policy{Grants: []Grant{
		{Resources: []string{}, Permissions: []string{"read"}},
	}}
	if err := Validate(p); err == nil {
		t.Fatal("expected error for empty resources")
	}
}

func TestValidate_EmptyPermissions(t *testing.T) {
	p := &Policy{Grants: []Grant{
		{Resources: []string{"*"}, Permissions: []string{}},
	}}
	if err := Validate(p); err == nil {
		t.Fatal("expected error for empty permissions")
	}
}
