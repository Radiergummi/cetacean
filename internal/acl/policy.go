package acl

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	json "github.com/goccy/go-json"
	"gopkg.in/yaml.v3"
)

// Grant is a single authorization grant: a tuple of resources, audience, and
// permissions. Provider-sourced grants omit audience (they are implicitly
// scoped to the authenticated user).
type Grant struct {
	Resources   []string `json:"resources"          yaml:"resources"          toml:"resources"`
	Audience    []string `json:"audience,omitempty" yaml:"audience,omitempty" toml:"audience,omitempty"`
	Permissions []string `json:"permissions"        yaml:"permissions"        toml:"permissions"`
}

// Policy holds a list of grants loaded from a file or inline string.
type Policy struct {
	Grants []Grant `json:"grants" yaml:"grants" toml:"grants"`
}

var validResourceTypes = map[string]bool{
	"service": true,
	"stack":   true,
	"node":    true,
	"task":    true,
	"config":  true,
	"secret":  true,
	"network": true,
	"volume":  true,
	"plugin":  true,
	"swarm":   true,
}

var validAudienceKinds = map[string]bool{
	"user":  true,
	"group": true,
}

var validPermissions = map[string]bool{
	"read":  true,
	"write": true,
}

// validateGrant checks a single grant for structural errors.
func validateGrant(g Grant) error {
	if len(g.Resources) == 0 {
		return fmt.Errorf("resources must not be empty")
	}

	for _, r := range g.Resources {
		if r == "*" {
			continue
		}

		parts := strings.SplitN(r, ":", 2)
		if len(parts) != 2 || parts[1] == "" {
			return fmt.Errorf("invalid resource expression %q (expected type:pattern)", r)
		}

		if !validResourceTypes[parts[0]] {
			return fmt.Errorf("unknown resource type %q", parts[0])
		}

		if _, err := path.Match(parts[1], ""); err != nil {
			return fmt.Errorf("invalid glob pattern in resource %q: %w", r, err)
		}
	}

	for _, a := range g.Audience {
		if a == "*" {
			continue
		}

		parts := strings.SplitN(a, ":", 2)
		if len(parts) != 2 || parts[1] == "" {
			return fmt.Errorf("invalid audience expression %q (expected kind:pattern)", a)
		}

		if !validAudienceKinds[parts[0]] {
			return fmt.Errorf("unknown audience kind %q", parts[0])
		}

		if _, err := path.Match(parts[1], ""); err != nil {
			return fmt.Errorf("invalid glob pattern in audience %q: %w", a, err)
		}
	}

	if len(g.Permissions) == 0 {
		return fmt.Errorf("permissions must not be empty")
	}

	for _, perm := range g.Permissions {
		if !validPermissions[perm] {
			return fmt.Errorf("unknown permission %q", perm)
		}
	}

	return nil
}

// Validate checks the policy for structural errors.
func Validate(p *Policy) error {
	for i, g := range p.Grants {
		if err := validateGrant(g); err != nil {
			return fmt.Errorf("grant %d: %w", i, err)
		}
	}

	return nil
}

// ParsePolicy auto-detects format (JSON, TOML, YAML) and parses a policy.
func ParsePolicy(data []byte) (*Policy, error) {
	// Try JSON first (fast, unambiguous).
	var p Policy
	if err := json.Unmarshal(data, &p); err == nil && len(p.Grants) > 0 {
		return &p, nil
	}

	// Try TOML (has brackets/equals that rarely appear in YAML).
	p = Policy{}
	if err := toml.Unmarshal(data, &p); err == nil && len(p.Grants) > 0 {
		return &p, nil
	}

	// Fall back to YAML.
	p = Policy{}
	if err := yaml.Unmarshal(data, &p); err == nil && len(p.Grants) > 0 {
		return &p, nil
	}

	return nil, fmt.Errorf("could not parse policy as JSON, TOML, or YAML")
}

// ParsePolicyFile reads a policy file and parses it by extension.
func ParsePolicyFile(path string) (*Policy, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("reading policy file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	var p Policy
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("parsing JSON policy: %w", err)
		}
	case ".toml":
		if err := toml.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("parsing TOML policy: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("parsing YAML policy: %w", err)
		}
	default:
		// Unknown extension: auto-detect.
		parsed, err := ParsePolicy(data)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	}
	return &p, nil
}
