package acl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePolicy_JSON(t *testing.T) {
	data := []byte(
		`{"grants":[{"resources":["*"],"audience":["group:ops"],"permissions":["read","write"]}]}`,
	)
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
	data := []byte(
		"grants:\n  - resources: [\"*\"]\n    audience: [\"group:ops\"]\n    permissions: [\"read\"]\n",
	)
	p, err := ParsePolicy(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(p.Grants))
	}
}

func TestParsePolicy_TOML(t *testing.T) {
	data := []byte(
		"[[grants]]\nresources = [\"*\"]\naudience = [\"group:ops\"]\npermissions = [\"read\"]\n",
	)
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
	if err := os.WriteFile(
		path,
		[]byte(
			`{"grants":[{"resources":["service:*"],"audience":["user:alice"],"permissions":["read"]}]}`,
		),
		0600,
	); err != nil {
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
		{
			Resources:   []string{"*"},
			Audience:    []string{"group:ops"},
			Permissions: []string{"read", "write"},
		},
		{
			Resources:   []string{"service:webapp-*"},
			Audience:    []string{"user:alice@example.com"},
			Permissions: []string{"read"},
		},
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

// Fix 4: {"grants": []} parses successfully but creates an empty policy.
// ParsePolicy requires len(p.Grants) > 0, so it fails to parse.
func TestParsePolicy_EmptyGrantsReturnsError(t *testing.T) {
	data := []byte(`{"grants":[]}`)
	_, err := ParsePolicy(data)
	if err == nil {
		t.Fatal("expected error for empty grants array -- ParsePolicy requires at least one grant")
	}
}

// Validate accepts an empty grants slice (no grants to validate).
func TestValidate_EmptyGrants(t *testing.T) {
	p := &Policy{Grants: []Grant{}}
	if err := Validate(p); err != nil {
		t.Fatalf("Validate should accept empty grants (nothing to validate): %v", err)
	}
}

func TestValidate_EmptyNameAfterColon(t *testing.T) {
	tests := []struct {
		name   string
		policy *Policy
	}{
		{
			name: "empty resource name",
			policy: &Policy{Grants: []Grant{
				{
					Resources:   []string{"service:"},
					Audience:    []string{"group:ops"},
					Permissions: []string{"read"},
				},
			}},
		},
		{
			name: "empty audience user value",
			policy: &Policy{Grants: []Grant{
				{
					Resources:   []string{"service:*"},
					Audience:    []string{"user:"},
					Permissions: []string{"read"},
				},
			}},
		},
		{
			name: "empty audience group value",
			policy: &Policy{Grants: []Grant{
				{
					Resources:   []string{"service:*"},
					Audience:    []string{"group:"},
					Permissions: []string{"read"},
				},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(tt.policy); err == nil {
				t.Fatal("expected validation error for empty value after colon")
			}
		})
	}
}

func TestParsePolicyFile_YAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	data := []byte(
		"grants:\n  - resources: [\"service:*\"]\n    audience: [\"user:alice\"]\n    permissions: [\"read\", \"write\"]\n",
	)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := ParsePolicyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(p.Grants))
	}
	g := p.Grants[0]
	if len(g.Resources) != 1 || g.Resources[0] != "service:*" {
		t.Fatalf("unexpected resources: %v", g.Resources)
	}
	if len(g.Permissions) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(g.Permissions))
	}
}

func TestParsePolicyFile_TOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.toml")
	data := []byte(
		"[[grants]]\nresources = [\"node:*\"]\naudience = [\"group:ops\"]\npermissions = [\"read\"]\n",
	)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := ParsePolicyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(p.Grants))
	}
	g := p.Grants[0]
	if len(g.Resources) != 1 || g.Resources[0] != "node:*" {
		t.Fatalf("unexpected resources: %v", g.Resources)
	}
	if len(g.Audience) != 1 || g.Audience[0] != "group:ops" {
		t.Fatalf("unexpected audience: %v", g.Audience)
	}
}
