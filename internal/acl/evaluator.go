package acl

import (
	"log/slog"
	"slices"
	"sync/atomic"

	"github.com/radiergummi/cetacean/internal/auth"
)

// Evaluator is the main entry point for ACL checks. A nil Evaluator or a nil
// policy means "allow all" — this preserves backward compatibility when no
// policy is configured.
type Evaluator struct {
	policy        atomic.Pointer[Policy]
	source        GrantSource
	resolver      ResourceResolver
	labelsEnabled bool
}

// NewEvaluator creates a new Evaluator. All parameters are optional.
func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

// SetPolicy atomically swaps the file-based policy.
func (e *Evaluator) SetPolicy(p *Policy) {
	if e == nil {
		return
	}
	e.policy.Store(p)
}

// SetResolver sets the resource resolver for stack/task resolution.
func (e *Evaluator) SetResolver(r ResourceResolver) {
	if e == nil {
		return
	}
	e.resolver = r
}

// SetSource sets the provider-specific grant source.
func (e *Evaluator) SetSource(s GrantSource) {
	if e == nil {
		return
	}
	e.source = s
}

// SetLabelsEnabled enables or disables label-based ACL evaluation.
func (e *Evaluator) SetLabelsEnabled(enabled bool) {
	if e == nil {
		return
	}
	e.labelsEnabled = enabled
}

// Can checks if the identity has the given permission on the resource.
// resource is "type:name", e.g. "service:webapp-api".
// A nil evaluator or nil policy means allow all.
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
		if !e.labelsEnabled {
			return items
		}
		p = &Policy{}
	}

	grants := e.collectGrants(id, p)
	var result []T
	for _, item := range items {
		resource := resourceFunc(item)

		// Check labels first when enabled.
		if e.labelsEnabled && e.resolver != nil {
			if allowed, handled := e.checkLabels(id, permission, resource); handled {
				if allowed {
					result = append(result, item)
				}
				continue
			}
		}

		for _, g := range grants {
			if hasPermission(g, permission) && e.grantMatchesResource(g, resource) {
				result = append(result, item)
				break
			}
		}
	}
	return result
}

// HasAnyGrant returns true if the identity has at least one grant in the policy.
// Used to gate cluster-wide endpoints.
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
	return len(grants) > 0 || e.labelsEnabled
}

// PermissionsFor returns a map of resource patterns to permission lists
// representing the effective permissions for the given identity. This is
// a projection of raw grant patterns, not resolved to actual resources.
func (e *Evaluator) PermissionsFor(id *auth.Identity) map[string][]string {
	if e == nil {
		return nil
	}
	p := e.policy.Load()
	if p == nil {
		return nil
	}

	grants := e.collectGrants(id, p)
	if len(grants) == 0 {
		return nil
	}

	result := make(map[string][]string)
	for _, g := range grants {
		for _, r := range g.Resources {
			existing := result[r]
			for _, perm := range g.Permissions {
				if !slices.Contains(existing, perm) {
					existing = append(existing, perm)
				}
			}
			result[r] = existing
		}
	}
	return result
}

// checkLabels evaluates label-based ACL for a resource. Returns (allowed, handled).
// If handled is true, the label result is authoritative for this resource+identity.
// If handled is false, the caller should fall through to config grants.
func (e *Evaluator) checkLabels(
	id *auth.Identity,
	permission string,
	resource string,
) (bool, bool) {
	labels := e.resolveLabels(resource)
	if labels == nil || !hasACLLabels(labels) {
		return false, false
	}

	readAudiences, writeAudiences := ParseACLLabels(labels)
	matchesWrite := matchLabelAudience(writeAudiences, id)
	matchesRead := matchLabelAudience(readAudiences, id)

	if matchesWrite || matchesRead {
		slog.Debug("ACL label grant matched",
			"resource", resource,
			"permission", permission,
			"matchedWrite", matchesWrite,
			"matchedRead", matchesRead,
		)
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

	// Identity doesn't match any label audience. Labels are present, so they
	// suppress implicit access — but don't block explicit config grants.
	subject := ""
	if id != nil {
		subject = id.Subject
	}
	slog.Debug("ACL labels present but no audience match",
		"resource", resource,
		"subject", subject,
	)
	return false, false
}

// resolveLabels returns the labels for a resource, resolving task→service
// inheritance.
func (e *Evaluator) resolveLabels(resource string) map[string]string {
	resType, resName, ok := splitResource(resource)
	if !ok {
		return nil
	}
	if resType == "task" {
		if svcName := e.resolver.ServiceOfTask(resName); svcName != "" {
			return e.resolver.LabelsOf("service", svcName)
		}
		return nil
	}
	return e.resolver.LabelsOf(resType, resName)
}

// collectGrants gathers all grants applicable to the identity: file-based
// grants where audience matches, plus provider-sourced grants.
func (e *Evaluator) collectGrants(id *auth.Identity, p *Policy) []Grant {
	var grants []Grant

	// File-based grants: check audience.
	for _, g := range p.Grants {
		if audienceMatches(g, id) {
			grants = append(grants, g)
		}
	}

	// Provider-sourced grants: skip audience check.
	if e.source != nil && id != nil {
		grants = append(grants, e.source.GrantsFor(id)...)
	}

	return grants
}

// grantMatchesResource checks if a grant covers the given resource,
// including stack resolution and task inheritance.
func (e *Evaluator) grantMatchesResource(g Grant, resource string) bool {
	for _, expr := range g.Resources {
		if matchResource(expr, resource) {
			return true
		}
	}

	// Stack resolution: if no direct match, check if the resource belongs
	// to a stack that a grant covers.
	if e.resolver != nil {
		resType, resID, ok := splitResource(resource)
		if ok {
			// Task inheritance: tasks inherit from their parent service.
			if resType == "task" {
				if svcName := e.resolver.ServiceOfTask(resID); svcName != "" {
					svcResource := "service:" + svcName
					for _, expr := range g.Resources {
						if matchResource(expr, svcResource) {
							return true
						}
					}
					// Also check the parent service's stack (task→service→stack).
					if stackName := e.resolver.StackOf("service", svcName); stackName != "" {
						stackResource := "stack:" + stackName
						for _, expr := range g.Resources {
							if matchResource(expr, stackResource) {
								return true
							}
						}
					}
				}
			}

			// Stack membership: check if the resource belongs to a matching stack.
			if stackName := e.resolver.StackOf(resType, resID); stackName != "" {
				stackResource := "stack:" + stackName
				for _, expr := range g.Resources {
					if matchResource(expr, stackResource) {
						return true
					}
				}
			}
		}
	}

	return false
}

func splitResource(resource string) (string, string, bool) {
	for i := range resource {
		if resource[i] == ':' {
			return resource[:i], resource[i+1:], true
		}
	}
	return "", "", false
}

func audienceMatches(g Grant, id *auth.Identity) bool {
	if len(g.Audience) == 0 {
		// Provider grants have no audience — they match implicitly.
		// File grants with no audience match everyone.
		return true
	}
	if id == nil {
		return false
	}
	for _, expr := range g.Audience {
		if matchAudience(expr, id) {
			return true
		}
	}
	return false
}

func hasPermission(g Grant, permission string) bool {
	for _, p := range g.Permissions {
		if p == permission {
			return true
		}
		// write implies read
		if permission == "read" && p == "write" {
			return true
		}
	}
	return false
}
