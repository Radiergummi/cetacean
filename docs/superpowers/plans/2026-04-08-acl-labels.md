# ACL Labels Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let teams control resource access via `cetacean.acl.read` / `cetacean.acl.write` Docker labels, using existing ACL audience syntax.

**Architecture:** Extend `ResourceResolver` with `LabelsOf()`, add label evaluation in the evaluator's `Can()` path (checked before config grants, with label audiences winning over config for matched identities), wire through a `labelsEnabled` flag from config. Add a Cetacean ACL integration panel on service detail pages.

**Tech Stack:** Go (backend ACL evaluator, config, cache resolver), React + TypeScript (integration panel)

---

### Task 1: Extend ResourceResolver with LabelsOf

**Files:**
- Modify: `internal/acl/source.go:14-22`
- Modify: `internal/cache/resolver.go`
- Modify: `internal/acl/evaluator_test.go:9-21` (stubResolver)

- [ ] **Step 1: Write the failing test**

Add a test in `internal/acl/evaluator_test.go` that verifies `LabelsOf` is called during evaluation. First, update the `stubResolver` to implement `LabelsOf`:

```go
type stubResolver struct {
	stacks   map[string]string            // "type:id" -> stack name
	services map[string]string            // taskID -> service name
	labels   map[string]map[string]string // "type:name" -> labels
}

func (r *stubResolver) LabelsOf(resourceType, name string) map[string]string {
	return r.labels[resourceType+":"+name]
}
```

This will fail to compile because `ResourceResolver` doesn't have `LabelsOf` yet.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/acl/ 2>&1 | head -20`
Expected: Compilation error — `stubResolver` doesn't satisfy `ResourceResolver` (missing `LabelsOf`), or `ResourceResolver` doesn't declare it yet.

- [ ] **Step 3: Add LabelsOf to ResourceResolver interface**

In `internal/acl/source.go`, add the new method to the interface:

```go
// ResourceResolver resolves cross-resource relationships for ACL evaluation.
type ResourceResolver interface {
	// StackOf returns the stack name for a resource, or "" if it doesn't
	// belong to a stack.
	StackOf(resourceType, resourceID string) string

	// ServiceOfTask returns the service name for a task, or "" if unknown.
	ServiceOfTask(taskID string) string

	// LabelsOf returns the labels for a resource, or nil if the resource
	// doesn't exist or has no labels. Used for label-based ACL evaluation.
	LabelsOf(resourceType, name string) map[string]string
}
```

- [ ] **Step 4: Implement LabelsOf in cache**

In `internal/cache/resolver.go`, add:

```go
// LabelsOf returns the labels for a resource identified by its display name,
// or nil if the resource doesn't exist. Used for label-based ACL evaluation.
func (c *Cache) LabelsOf(resourceType, name string) map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	switch resourceType {
	case "service":
		for _, s := range c.services {
			if s.Spec.Name == name {
				return s.Spec.Labels
			}
		}
	case "config":
		for _, cfg := range c.configs.items {
			if cfg.Spec.Name == name {
				return cfg.Spec.Labels
			}
		}
	case "secret":
		for _, s := range c.secrets.items {
			if s.Spec.Name == name {
				return s.Spec.Labels
			}
		}
	case "network":
		for _, n := range c.networks.items {
			if n.Name == name {
				return n.Labels
			}
		}
	case "volume":
		for _, v := range c.volumes.items {
			if v.Name == name {
				return v.Labels
			}
		}
	case "node":
		for _, n := range c.nodes {
			hostname := n.Description.Hostname
			if hostname == "" {
				hostname = n.ID
			}
			if hostname == name {
				return n.Spec.Labels
			}
		}
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify compilation passes**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/acl/ ./internal/cache/ 2>&1 | tail -5`
Expected: All existing tests PASS (no behavior change yet).

- [ ] **Step 6: Commit**

```bash
git add internal/acl/source.go internal/cache/resolver.go internal/acl/evaluator_test.go
git commit -m "acl: add LabelsOf to ResourceResolver interface and cache implementation"
```

---

### Task 2: Add labelsEnabled flag to config and evaluator

**Files:**
- Modify: `internal/config/acl.go`
- Modify: `internal/config/file.go:156-162` (fileACL struct)
- Modify: `internal/acl/evaluator.go:13-17` (Evaluator struct)
- Modify: `main.go` (ACL init block around line 284)

- [ ] **Step 1: Add Labels field to ACLConfig and fileACL**

In `internal/config/file.go`, add to `fileACL`:

```go
type fileACL struct {
	Policy              *string `toml:"policy"`
	PolicyFile          *string `toml:"policy_file"`
	Labels              *bool   `toml:"labels"`
	TailscaleCapability *string `toml:"tailscale_capability"`
	OIDCClaim           *string `toml:"oidc_claim"`
	HeadersACL          *string `toml:"headers_acl"`
}
```

In `internal/config/acl.go`, add to `ACLConfig` and `LoadACL`:

```go
type ACLConfig struct {
	Policy              string
	PolicyFile          string
	Labels              bool   // enable label-based ACL evaluation
	TailscaleCapability string
	OIDCClaim           string
	HeadersACL          string
}
```

In `LoadACL`, add the `Labels` field resolution:

```go
func LoadACL(flags *Flags, fc *fileConfig) ACLConfig {
	if flags == nil {
		flags = &Flags{}
	}

	var fPolicy, fPolicyFile *string
	var fLabels *bool
	var fTailscaleCap, fOIDCClaim, fHeadersACL *string
	if fc != nil && fc.ACL != nil {
		fPolicy = fc.ACL.Policy
		fPolicyFile = fc.ACL.PolicyFile
		fLabels = fc.ACL.Labels
		fTailscaleCap = fc.ACL.TailscaleCapability
		fOIDCClaim = fc.ACL.OIDCClaim
		fHeadersACL = fc.ACL.HeadersACL
	}

	return ACLConfig{
		Policy: resolve(flags.ACLPolicy, "CETACEAN_ACL_POLICY", fPolicy, ""),
		PolicyFile: resolve(
			flags.ACLPolicyFile,
			"CETACEAN_ACL_POLICY_FILE",
			fPolicyFile,
			"",
		),
		Labels:              resolveBool(nil, "CETACEAN_ACL_LABELS", fLabels, false),
		TailscaleCapability: resolve(nil, "CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY", fTailscaleCap, ""),
		OIDCClaim:           resolve(nil, "CETACEAN_AUTH_OIDC_ACL_CLAIM", fOIDCClaim, ""),
		HeadersACL:          resolve(nil, "CETACEAN_AUTH_HEADERS_ACL", fHeadersACL, ""),
	}
}
```

- [ ] **Step 2: Add EnableLabels to evaluator**

In `internal/acl/evaluator.go`, add a field and setter:

```go
type Evaluator struct {
	policy       atomic.Pointer[Policy]
	source       GrantSource
	resolver     ResourceResolver
	labelsEnabled bool
}
```

Add setter method after `SetSource`:

```go
// SetLabelsEnabled enables or disables label-based ACL evaluation.
func (e *Evaluator) SetLabelsEnabled(enabled bool) {
	if e == nil {
		return
	}
	e.labelsEnabled = enabled
}
```

- [ ] **Step 3: Wire config to evaluator in main.go**

In `main.go`, after `aclEval.SetResolver(stateCache)` (around line 285), add:

```go
aclEval.SetLabelsEnabled(aclCfg.Labels)
```

Add a log line nearby:

```go
if aclCfg.Labels {
	slog.Info("label-based ACL evaluation enabled")
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build . && go test ./internal/... 2>&1 | tail -5`
Expected: PASS — no behavior change yet, just plumbing.

- [ ] **Step 5: Commit**

```bash
git add internal/config/acl.go internal/config/file.go internal/acl/evaluator.go main.go
git commit -m "acl: add CETACEAN_ACL_LABELS config flag and evaluator plumbing"
```

---

### Task 3: Implement label parsing

**Files:**
- Create: `internal/acl/labels.go`
- Create: `internal/acl/labels_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/acl/labels_test.go`:

```go
package acl

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

func TestParseACLLabels_ReadOnly(t *testing.T) {
	labels := map[string]string{
		"cetacean.acl.read": "group:dev,user:alice@example.com",
	}
	read, write := parseACLLabels(labels)
	if len(read) != 2 {
		t.Fatalf("expected 2 read audiences, got %d", len(read))
	}
	if read[0] != "group:dev" || read[1] != "user:alice@example.com" {
		t.Fatalf("unexpected read audiences: %v", read)
	}
	if len(write) != 0 {
		t.Fatalf("expected 0 write audiences, got %d", len(write))
	}
}

func TestParseACLLabels_ReadAndWrite(t *testing.T) {
	labels := map[string]string{
		"cetacean.acl.read":  "group:*",
		"cetacean.acl.write": "group:ops",
	}
	read, write := parseACLLabels(labels)
	if len(read) != 1 || read[0] != "group:*" {
		t.Fatalf("unexpected read: %v", read)
	}
	if len(write) != 1 || write[0] != "group:ops" {
		t.Fatalf("unexpected write: %v", write)
	}
}

func TestParseACLLabels_Whitespace(t *testing.T) {
	labels := map[string]string{
		"cetacean.acl.read": " group:dev , user:bob ",
	}
	read, _ := parseACLLabels(labels)
	if len(read) != 2 || read[0] != "group:dev" || read[1] != "user:bob" {
		t.Fatalf("whitespace not trimmed: %v", read)
	}
}

func TestParseACLLabels_EmptyAndMissing(t *testing.T) {
	// No ACL labels at all.
	read, write := parseACLLabels(map[string]string{"foo": "bar"})
	if read != nil || write != nil {
		t.Fatal("expected nil for labels without ACL entries")
	}

	// Empty value.
	read, write = parseACLLabels(map[string]string{"cetacean.acl.read": ""})
	if len(read) != 0 {
		t.Fatalf("expected 0 audiences from empty value, got %d", len(read))
	}
}

func TestHasACLLabels(t *testing.T) {
	if hasACLLabels(map[string]string{"foo": "bar"}) {
		t.Fatal("should not detect ACL labels")
	}
	if !hasACLLabels(map[string]string{"cetacean.acl.read": "group:dev"}) {
		t.Fatal("should detect ACL labels")
	}
	if !hasACLLabels(map[string]string{"cetacean.acl.write": "group:ops"}) {
		t.Fatal("should detect ACL labels")
	}
}

func TestMatchLabelAudience(t *testing.T) {
	alice := &auth.Identity{Subject: "alice", Email: "alice@example.com", Groups: []string{"dev", "ops"}}

	tests := []struct {
		name      string
		audiences []string
		want      bool
	}{
		{"user match", []string{"user:alice"}, true},
		{"email match", []string{"user:*@example.com"}, true},
		{"group match", []string{"group:dev"}, true},
		{"wildcard", []string{"*"}, true},
		{"no match", []string{"user:bob", "group:marketing"}, false},
		{"empty list", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchLabelAudience(tt.audiences, alice)
			if got != tt.want {
				t.Errorf("matchLabelAudience(%v) = %v, want %v", tt.audiences, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/acl/ 2>&1 | head -10`
Expected: Compilation error — `parseACLLabels`, `hasACLLabels`, `matchLabelAudience` undefined.

- [ ] **Step 3: Implement label parsing**

Create `internal/acl/labels.go`:

```go
package acl

import (
	"log/slog"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
)

const (
	labelRead  = "cetacean.acl.read"
	labelWrite = "cetacean.acl.write"
)

// hasACLLabels returns true if the label map contains any cetacean.acl.* key.
func hasACLLabels(labels map[string]string) bool {
	_, hasRead := labels[labelRead]
	_, hasWrite := labels[labelWrite]
	return hasRead || hasWrite
}

// parseACLLabels extracts read and write audience lists from labels.
// Returns nil, nil if no ACL labels are present.
func parseACLLabels(labels map[string]string) (read, write []string) {
	readVal, hasRead := labels[labelRead]
	writeVal, hasWrite := labels[labelWrite]

	if !hasRead && !hasWrite {
		return nil, nil
	}

	if hasRead {
		read = parseAudienceList(readVal)
	}
	if hasWrite {
		write = parseAudienceList(writeVal)
	}
	return read, write
}

// parseAudienceList splits a comma-separated audience string, trims whitespace,
// and drops empty entries.
func parseAudienceList(value string) []string {
	parts := strings.Split(value, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Warn on invalid audience expressions but include them — matchAudience
		// will reject them at evaluation time.
		if p != "*" {
			kind, _, ok := strings.Cut(p, ":")
			if !ok || (kind != "user" && kind != "group") {
				slog.Warn("invalid audience expression in ACL label", "expression", p)
			}
		}

		result = append(result, p)
	}
	return result
}

// matchLabelAudience checks if any audience expression matches the identity.
// Reuses the existing matchAudience function from match.go.
func matchLabelAudience(audiences []string, id *auth.Identity) bool {
	if id == nil {
		return false
	}
	for _, expr := range audiences {
		if matchAudience(expr, id) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/acl/ -run TestParseACLLabels -v && go test ./internal/acl/ -run TestHasACLLabels -v && go test ./internal/acl/ -run TestMatchLabelAudience -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/acl/labels.go internal/acl/labels_test.go
git commit -m "acl: add label parsing and audience matching for ACL labels"
```

---

### Task 4: Implement label evaluation in Can()

**Files:**
- Modify: `internal/acl/evaluator.go:51-73` (Can method)
- Modify: `internal/acl/evaluator.go:76-103` (Filter function)

- [ ] **Step 1: Write failing tests for label-based access**

Add to `internal/acl/evaluator_test.go`:

```go
func TestEvaluator_LabelGrant_ReadOnly(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetPolicy(&Policy{Grants: []Grant{}}) // Empty policy, default-deny
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {
				"cetacean.acl.read": "group:dev",
			},
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
			"service:webapp": {
				"cetacean.acl.write": "group:ops",
			},
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
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"group:dev"},
			Permissions: []string{"write"},
		},
	}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {
				"cetacean.acl.read":  "group:*",
				"cetacean.acl.write": "group:ops",
			},
		},
	})

	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	ops := &auth.Identity{Subject: "bob", Groups: []string{"ops"}}

	// dev has config write on service:*, but label narrows to read
	if !e.Can(dev, "read", "service:webapp") {
		t.Fatal("dev should be able to read via label group:*")
	}
	if e.Can(dev, "write", "service:webapp") {
		t.Fatal("dev should NOT be able to write (label narrows config)")
	}

	// ops matches label write
	if !e.Can(ops, "write", "service:webapp") {
		t.Fatal("ops should be able to write via label")
	}

	// dev can still write to other services (no labels, config applies)
	if !e.Can(dev, "write", "service:other") {
		t.Fatal("dev should still have config write on non-labeled services")
	}
}

func TestEvaluator_LabelSuppressesAllowAll(t *testing.T) {
	e := NewEvaluator()
	e.SetLabelsEnabled(true)
	// nil policy = allow-all mode, but labels should restrict
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:sensitive": {
				"cetacean.acl.read": "group:ops",
			},
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
		{
			Resources:   []string{"service:webapp"},
			Audience:    []string{"user:bot"},
			Permissions: []string{"write"},
		},
	}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {
				"cetacean.acl.read": "group:dev",
			},
		},
	})

	bot := &auth.Identity{Subject: "bot"}
	// bot not in label audience, but has explicit config grant
	if !e.Can(bot, "write", "service:webapp") {
		t.Fatal("bot should have write via explicit config grant")
	}
}

func TestEvaluator_LabelDisabled(t *testing.T) {
	e := NewEvaluator()
	// labelsEnabled defaults to false
	e.SetPolicy(&Policy{Grants: []Grant{}})
	e.SetResolver(&stubResolver{
		labels: map[string]map[string]string{
			"service:webapp": {
				"cetacean.acl.read": "group:dev",
			},
		},
	})

	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	// With labels disabled, empty policy = deny (no grants match)
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
			"service:webapp": {
				"cetacean.acl.read": "group:dev",
			},
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
			"service:webapp": {
				"cetacean.acl.read":  "group:dev",
				"cetacean.acl.write": "group:dev",
			},
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
			"service:visible": {
				"cetacean.acl.read": "group:dev",
			},
			"service:hidden": {
				"cetacean.acl.read": "group:ops",
			},
		},
	})

	type svc struct{ name string }
	items := []svc{{name: "visible"}, {name: "hidden"}, {name: "unlabeled"}}

	dev := &auth.Identity{Subject: "alice", Groups: []string{"dev"}}
	filtered := Filter(e, dev, "read", items, func(s svc) string { return "service:" + s.name })

	// visible: label grants read to dev ✓
	// hidden: label grants read to ops only ✗
	// unlabeled: no labels, empty policy = deny ✗
	if len(filtered) != 1 || filtered[0].name != "visible" {
		t.Fatalf("expected [visible], got %v", filtered)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/acl/ -run "TestEvaluator_Label|TestFilter_WithLabels" 2>&1 | head -20`
Expected: FAIL — label evaluation not implemented yet, tests checking label-based access will fail.

- [ ] **Step 3: Implement label evaluation in Can()**

Replace the `Can()` method in `internal/acl/evaluator.go`:

```go
// Can checks if the identity has the given permission on the resource.
// resource is "type:name", e.g. "service:webapp-api".
// A nil evaluator or nil policy means allow all.
func (e *Evaluator) Can(id *auth.Identity, permission string, resource string) bool {
	if e == nil {
		return true
	}
	p := e.policy.Load()
	if p == nil {
		return true
	}

	// Label evaluation: check resource labels first when enabled.
	if e.labelsEnabled && e.resolver != nil {
		if allowed, handled := e.checkLabels(id, permission, resource); handled {
			return allowed
		}
	}

	// Collect and check config/provider grants.
	grants := e.collectGrants(id, p)
	for _, g := range grants {
		if !hasPermission(g, permission) {
			continue
		}
		if e.grantMatchesResource(g, resource) {
			return true
		}
	}
	return false
}

// checkLabels evaluates label-based ACL for a resource.
// Returns (allowed, handled). If handled is true, the label decision is final
// for identities that match a label audience. If handled is false, the caller
// should fall through to config/provider grants.
func (e *Evaluator) checkLabels(id *auth.Identity, permission string, resource string) (bool, bool) {
	labels := e.resolveLabels(resource)
	if labels == nil || !hasACLLabels(labels) {
		return false, false
	}

	readAudiences, writeAudiences := parseACLLabels(labels)

	// Check if identity matches any label audience.
	matchesWrite := matchLabelAudience(writeAudiences, id)
	matchesRead := matchLabelAudience(readAudiences, id)

	if matchesWrite || matchesRead {
		// Identity matched a label audience — label determines permission.
		// Most permissive wins (additive within label layer).
		effectiveWrite := matchesWrite
		effectiveRead := matchesRead || matchesWrite // write implies read

		switch permission {
		case "write":
			return effectiveWrite, true
		case "read":
			return effectiveRead, true
		default:
			return false, true
		}
	}

	// Identity not in any label audience. Labels are present, so suppress
	// the implicit allow-all default. Config/provider grants with explicit
	// resource matches can still grant access, but the implicit allow-all
	// (nil/empty policy) default is suppressed — handled by the caller
	// checking config grants which won't match when policy is empty.
	//
	// We return handled=false to let the caller check config grants.
	// The key difference: when labels are present but don't match, and
	// there's no active policy (or the policy has no matching grant),
	// the result is deny. This is enforced by ensuring we never return
	// true from the allow-all checks at the top of Can() — those only
	// fire when policy is nil, but we've already loaded a non-nil policy.
	return false, false
}

// resolveLabels returns the labels for a resource, following task inheritance.
func (e *Evaluator) resolveLabels(resource string) map[string]string {
	resType, resName, ok := splitResource(resource)
	if !ok {
		return nil
	}

	// Tasks inherit labels from their parent service.
	if resType == "task" {
		if svcName := e.resolver.ServiceOfTask(resName); svcName != "" {
			return e.resolver.LabelsOf("service", svcName)
		}
		return nil
	}

	return e.resolver.LabelsOf(resType, resName)
}
```

- [ ] **Step 4: Update Filter() to handle labels**

The current `Filter()` function has an early return for nil policy that skips label evaluation. Update it in `internal/acl/evaluator.go`:

```go
// Filter returns only items the identity can access with the given permission.
func Filter[T any](
	e *Evaluator,
	id *auth.Identity,
	permission string,
	items []T,
	resourceFunc func(T) string,
) []T {
	if e == nil {
		return items
	}
	p := e.policy.Load()
	if p == nil {
		return items
	}

	// When labels are enabled, we must check each item individually via Can()
	// because label evaluation is per-resource.
	if e.labelsEnabled && e.resolver != nil {
		var result []T
		for _, item := range items {
			if e.Can(id, permission, resourceFunc(item)) {
				result = append(result, item)
			}
		}
		return result
	}

	grants := e.collectGrants(id, p)
	var result []T
	for _, item := range items {
		resource := resourceFunc(item)
		for _, g := range grants {
			if hasPermission(g, permission) && e.grantMatchesResource(g, resource) {
				result = append(result, item)
				break
			}
		}
	}
	return result
}
```

- [ ] **Step 5: Handle label suppression of allow-all**

There's a subtle issue: when labels are enabled but no config policy exists, `Can()` returns `true` at the nil-policy check before labels are evaluated. We need `Can()` to proceed to label evaluation even when there's no policy. The cleanest approach: when `labelsEnabled` is true, treat a nil policy as an empty policy (default-deny base, labels provide grants).

Update the top of `Can()`:

```go
func (e *Evaluator) Can(id *auth.Identity, permission string, resource string) bool {
	if e == nil {
		return true
	}
	p := e.policy.Load()

	// When labels are enabled, treat nil policy as empty (labels may restrict).
	// When labels are disabled, nil policy means allow-all (backward compat).
	if p == nil {
		if !e.labelsEnabled {
			return true
		}
		p = &Policy{}
	}

	// Label evaluation: check resource labels first when enabled.
	if e.labelsEnabled && e.resolver != nil {
		if allowed, handled := e.checkLabels(id, permission, resource); handled {
			return allowed
		}
	}

	// Collect and check config/provider grants.
	grants := e.collectGrants(id, p)
	for _, g := range grants {
		if !hasPermission(g, permission) {
			continue
		}
		if e.grantMatchesResource(g, resource) {
			return true
		}
	}
	return false
}
```

Apply the same nil-policy handling to `Filter()`:

```go
func Filter[T any](
	e *Evaluator,
	id *auth.Identity,
	permission string,
	items []T,
	resourceFunc func(T) string,
) []T {
	if e == nil {
		return items
	}
	p := e.policy.Load()
	if p == nil {
		if !e.labelsEnabled {
			return items
		}
		p = &Policy{}
	}

	if e.labelsEnabled && e.resolver != nil {
		var result []T
		for _, item := range items {
			if e.Can(id, permission, resourceFunc(item)) {
				result = append(result, item)
			}
		}
		return result
	}

	grants := e.collectGrants(id, p)
	var result []T
	for _, item := range items {
		resource := resourceFunc(item)
		for _, g := range grants {
			if hasPermission(g, permission) && e.grantMatchesResource(g, resource) {
				result = append(result, item)
				break
			}
		}
	}
	return result
}
```

Also update `HasAnyGrant()` similarly — when labels are enabled, any identity might have label grants:

```go
func (e *Evaluator) HasAnyGrant(id *auth.Identity) bool {
	if e == nil {
		return true
	}
	p := e.policy.Load()
	if p == nil {
		if !e.labelsEnabled {
			return true
		}
		p = &Policy{}
	}
	grants := e.collectGrants(id, p)
	// When labels are enabled, identities may have label-based grants
	// even without config grants. Allow access to cluster-wide endpoints
	// so they can discover which resources they have label grants on.
	return len(grants) > 0 || e.labelsEnabled
}
```

- [ ] **Step 6: Run all tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/acl/ -v 2>&1 | tail -30`
Expected: All tests PASS — both new label tests and all existing tests.

- [ ] **Step 7: Commit**

```bash
git add internal/acl/evaluator.go internal/acl/evaluator_test.go
git commit -m "acl: implement label-based evaluation in Can() and Filter()"
```

---

### Task 5: Add observability logging

**Files:**
- Modify: `internal/acl/evaluator.go` (checkLabels method)

- [ ] **Step 1: Add debug logging to checkLabels**

Add `"log/slog"` to the imports in `internal/acl/evaluator.go`, then update `checkLabels`:

After `matchesWrite` and `matchesRead` are computed, before the `if matchesWrite || matchesRead` check, add:

```go
	if matchesWrite || matchesRead {
		slog.Debug("ACL label grant matched",
			"resource", resource,
			"permission", permission,
			"matchedWrite", matchesWrite,
			"matchedRead", matchesRead,
		)
```

After the identity-not-matched block (before the final `return false, false`):

```go
	slog.Debug("ACL labels present but no audience match",
		"resource", resource,
		"subject", id.Subject,
	)
```

The warning for invalid audience expressions is already in `parseAudienceList` in `labels.go`.

- [ ] **Step 2: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/acl/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/acl/evaluator.go
git commit -m "acl: add debug logging for label-based ACL decisions"
```

---

### Task 6: Backend integration detection for ACL labels

**Files:**
- Create: `internal/integrations/acl.go`
- Modify: `internal/integrations/integrations.go`

- [ ] **Step 1: Write the integration detector**

Create `internal/integrations/acl.go`:

```go
package integrations

// ACLIntegration represents parsed Cetacean ACL label configuration.
type ACLIntegration struct {
	Name  string   `json:"name"`
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

func detectACL(labels map[string]string) *ACLIntegration {
	readVal, hasRead := labels["cetacean.acl.read"]
	writeVal, hasWrite := labels["cetacean.acl.write"]

	if !hasRead && !hasWrite {
		return nil
	}

	integration := &ACLIntegration{Name: "cetacean-acl"}

	if hasRead {
		integration.Read = splitAudiences(readVal)
	}
	if hasWrite {
		integration.Write = splitAudiences(writeVal)
	}

	return integration
}

func splitAudiences(value string) []string {
	if value == "" {
		return nil
	}

	var result []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
```

Note: the file needs `import "strings"` at the top.

- [ ] **Step 2: Register in Detect()**

In `internal/integrations/integrations.go`, add the ACL detector:

```go
func Detect(labels map[string]string) []any {
	var integrations []any

	if t := detectTraefik(labels); t != nil {
		integrations = append(integrations, t)
	}

	if s := detectShepherd(labels); s != nil {
		integrations = append(integrations, s)
	}

	if c := detectCronjob(labels); c != nil {
		integrations = append(integrations, c)
	}

	if d := detectDiun(labels); d != nil {
		integrations = append(integrations, d)
	}

	if a := detectACL(labels); a != nil {
		integrations = append(integrations, a)
	}

	return integrations
}
```

- [ ] **Step 3: Run tests and build**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build . 2>&1 | tail -5`
Expected: Compiles successfully.

- [ ] **Step 4: Commit**

```bash
git add internal/integrations/acl.go internal/integrations/integrations.go
git commit -m "integrations: detect cetacean.acl.* labels as integration panel data"
```

---

### Task 7: Frontend types and integration registration

**Files:**
- Modify: `frontend/src/api/types.ts:348-352`
- Modify: `frontend/src/lib/integrationLabels.ts:60-67`

- [ ] **Step 1: Add TypeScript types**

In `frontend/src/api/types.ts`, add the interface before the `Integration` union type:

```typescript
export interface AclIntegration {
  name: "cetacean-acl";
  read?: string[];
  write?: string[];
}
```

Update the `Integration` union type:

```typescript
export type Integration =
  | TraefikIntegration
  | ShepherdIntegration
  | CronjobIntegration
  | DiunIntegration
  | AclIntegration;
```

- [ ] **Step 2: Register label prefix**

In `frontend/src/lib/integrationLabels.ts`, update `integrationLabelPrefix`:

```typescript
export const integrationLabelPrefix = {
  traefik: "traefik.",
  shepherd: "shepherd.",
  "swarm-cronjob": "swarm.cronjob.",
  diun: "diun.",
  "cetacean-acl": "cetacean.acl.",
} as const;
```

- [ ] **Step 3: Run type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit 2>&1 | tail -10`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/lib/integrationLabels.ts
git commit -m "frontend: add ACL integration types and label prefix registration"
```

---

### Task 8: ACL integration panel component

**Files:**
- Create: `frontend/src/components/service-detail/AclPanel.tsx`
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Create the panel component**

Create `frontend/src/components/service-detail/AclPanel.tsx`:

```tsx
import { IntegrationSection } from "./IntegrationSection";
import type { AclIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { saveIntegrationLabels } from "@/lib/integrationLabels";
import { useState } from "react";
import { X, Plus } from "lucide-react";

/**
 * Panel displaying parsed Cetacean ACL label configuration,
 * with optional inline editing support.
 */
export function AclPanel({
  integration,
  rawLabels,
  serviceId,
  onSaved,
  editable,
}: {
  integration: AclIntegration;
  rawLabels: [string, string][];
  serviceId: string;
  onSaved: (updated: Record<string, string>) => void;
  editable?: boolean;
}) {
  const { read, write } = integration;

  const [formRead, setFormRead] = useState<string[]>(read ?? []);
  const [formWrite, setFormWrite] = useState<string[]>(write ?? []);

  function resetForm() {
    setFormRead(integration.read ?? []);
    setFormWrite(integration.write ?? []);
  }

  function serializeToLabels(): Record<string, string> {
    const labels: Record<string, string> = {};

    const readFiltered = formRead.filter((audience) => audience.trim());
    const writeFiltered = formWrite.filter((audience) => audience.trim());

    if (readFiltered.length > 0) {
      labels["cetacean.acl.read"] = readFiltered.join(",");
    }
    if (writeFiltered.length > 0) {
      labels["cetacean.acl.write"] = writeFiltered.join(",");
    }

    return labels;
  }

  async function handleSave() {
    await saveIntegrationLabels(rawLabels, serializeToLabels(), serviceId, onSaved);
  }

  const editForm = (
    <div className="space-y-4">
      <AudienceListEditor
        label="Read"
        audiences={formRead}
        onChange={setFormRead}
      />
      <AudienceListEditor
        label="Write"
        audiences={formWrite}
        onChange={setFormWrite}
      />
    </div>
  );

  const rows: ([string, string] | false)[] = [
    read && read.length > 0 && ["Read", read.join(", ")],
    write && write.length > 0 && ["Write", write.join(", ")],
  ];

  return (
    <IntegrationSection
      title="Access Control"
      defaultOpen
      enabled
      rawLabels={rawLabels}
      editable={editable}
      editContent={editForm}
      onEditStart={resetForm}
      onSave={handleSave}
      serviceId={serviceId}
      onRawSave={onSaved}
    >
      <KVTable rows={rows} />
    </IntegrationSection>
  );
}

function AudienceListEditor({
  label,
  audiences,
  onChange,
}: {
  label: string;
  audiences: string[];
  onChange: (audiences: string[]) => void;
}) {
  function addEntry() {
    onChange([...audiences, ""]);
  }

  function removeEntry(index: number) {
    onChange(audiences.filter((_, entryIndex) => entryIndex !== index));
  }

  function updateEntry(index: number, value: string) {
    const updated = [...audiences];
    updated[index] = value;
    onChange(updated);
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between">
        <label className="text-xs font-medium text-foreground">{label}</label>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 px-2 text-xs"
          onClick={addEntry}
        >
          <Plus className="mr-1 h-3 w-3" />
          Add
        </Button>
      </div>
      {audiences.map((audience, index) => (
        <div
          key={index}
          className="flex items-center gap-1.5"
        >
          <Input
            value={audience}
            onChange={(event) => updateEntry(index, event.target.value)}
            placeholder="group:ops or user:alice@example.com"
            className="font-mono text-xs"
          />
          <Button
            variant="ghost"
            size="sm"
            className="h-8 w-8 shrink-0 p-0"
            onClick={() => removeEntry(index)}
          >
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      ))}
      {audiences.length === 0 && (
        <p className="text-xs text-muted-foreground">No audiences configured</p>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Wire into ServiceDetail**

In `frontend/src/pages/ServiceDetail.tsx`, add the import:

```typescript
import { AclPanel } from "../components/service-detail/AclPanel";
```

In the integrations switch statement (around line 545), add a new case before the default:

```tsx
          case "cetacean-acl":
            return (
              <AclPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
```

- [ ] **Step 3: Run type check and lint**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npm run lint 2>&1 | tail -10`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/AclPanel.tsx frontend/src/pages/ServiceDetail.tsx
git commit -m "frontend: add ACL integration panel for viewing and editing ACL labels"
```

---

### Task 9: Update documentation

**Files:**
- Modify: `docs/authorization.md`
- Modify: `docs/integrations.md`
- Modify: `docs/configuration.md`

- [ ] **Step 1: Add label-based ACL section to authorization docs**

In `docs/authorization.md`, add a new section for label-based ACL covering: label format, precedence rules, task inheritance, examples, and the `CETACEAN_ACL_LABELS` config flag.

- [ ] **Step 2: Add Cetacean ACL to integrations docs**

In `docs/integrations.md`, add a row to the supported tools table:

```markdown
| [Cetacean ACL](authorization.md#label-based-access-control) | `cetacean.acl.*` | Resource-level access control. Shows read/write audience lists. |
```

- [ ] **Step 3: Add CETACEAN_ACL_LABELS to configuration docs**

In `docs/configuration.md`, add the new env var to the environment variables table.

- [ ] **Step 4: Commit**

```bash
git add docs/authorization.md docs/integrations.md docs/configuration.md
git commit -m "docs: document label-based ACL feature"
```

---

### Task 10: End-to-end verification

- [ ] **Step 1: Run all backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... 2>&1 | tail -20`
Expected: All PASS.

- [ ] **Step 2: Run frontend checks**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npm run lint && npm run fmt:check 2>&1 | tail -10`
Expected: No errors.

- [ ] **Step 3: Full build**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run build && cd .. && go build -o cetacean . 2>&1 | tail -5`
Expected: Build succeeds.

- [ ] **Step 4: Run make check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make check 2>&1 | tail -20`
Expected: All checks pass.
