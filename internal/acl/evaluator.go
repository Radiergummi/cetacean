package acl

import (
	"slices"
	"sync/atomic"

	"github.com/radiergummi/cetacean/internal/auth"
)

// Evaluator is the main entry point for ACL checks. A nil Evaluator or a nil
// policy means "allow all" — this preserves backward compatibility when no
// policy is configured.
type Evaluator struct {
	policy   atomic.Pointer[Policy]
	source   GrantSource
	resolver ResourceResolver
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

	// Collect all matching grants.
	grants := e.collectGrants(id, p)

	// Check if any grant covers the resource and permission.
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
		return items
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

// HasAnyGrant returns true if the identity has at least one grant in the policy.
// Used to gate cluster-wide endpoints.
func (e *Evaluator) HasAnyGrant(id *auth.Identity) bool {
	if e == nil {
		return true
	}
	p := e.policy.Load()
	if p == nil {
		return true
	}
	grants := e.collectGrants(id, p)
	return len(grants) > 0
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

// impliedTypes names, for a grant's resource type, the other resource types a
// grant on it also covers — the type-level shadow of grantMatchesResource's
// resolver walk. A stack grant reaches every resource StackOf can place in a
// stack; a service grant reaches that service's tasks. Nothing reaches nodes,
// plugins or the swarm itself, which belong to no stack.
//
// Keep in step with grantMatchesResource. TestTypeGrantsAgreesWithCan is the
// guard: it drives the real evaluator over a resolver holding one resource of
// each type and fails if the two disagree in either direction.
var impliedTypes = map[string][]string{
	"stack":   {"service", "task", "config", "secret", "network", "volume"},
	"service": {"task"},
}

// TypeAccess is the type-level projection of an identity's grants: which
// resource *types* it may exercise a permission on, without naming a resource.
//
// It exists because grantMatchesResource needs a concrete resource name to
// resolve stack membership and task parentage, while callers that filter a
// catalog — a tool list, a notification subscription — have only a type. The
// projection is deliberately an over-approximation in exactly the way a
// pattern already is: "service:web-*" reports the service type whether or not
// a matching service exists. It answers "could this identity ever read a
// service?", never "may it read this one" — Can remains the only authority
// for that, and every call site still checks it.
type TypeAccess struct {
	// byPermission is permission → resource type → granted. The "*" key means
	// every type; Can resolves it, so callers never see it.
	byPermission map[string]map[string]bool

	// allowAll mirrors Can's allow-all: a nil evaluator or no policy loaded.
	allowAll bool

	// anyGrant reports whether the identity matched at least one grant. It is
	// what separates "no policy, allow everything" from "policy loaded, this
	// caller matched nothing" — a distinction PermissionsFor's nil return
	// cannot express, and the reason this type carries both.
	anyGrant bool
}

// AllowAll reports that no policy is in force, so every type is permitted.
func (t TypeAccess) AllowAll() bool { return t.allowAll }

// HasAnyGrant reports whether the identity matched at least one grant.
func (t TypeAccess) HasAnyGrant() bool { return t.anyGrant }

// Can reports whether the identity may exercise permission on some resource of
// resourceType. Write implies read, as it does in hasPermission.
func (t TypeAccess) Can(permission, resourceType string) bool {
	if t.allowAll {
		return true
	}

	if t.granted(permission, resourceType) {
		return true
	}

	return permission == "read" && t.granted("write", resourceType)
}

func (t TypeAccess) granted(permission, resourceType string) bool {
	byType := t.byPermission[permission]
	if byType == nil {
		return false
	}

	return byType[resourceType] || byType["*"]
}

// TypeGrants projects an identity's grants onto the types it can act on,
// expanding them the way Can expands them at call time. The whole answer comes
// from one policy read, so a hot reload cannot land between two questions and
// answer each from a different policy.
func (e *Evaluator) TypeGrants(id *auth.Identity) TypeAccess {
	if e == nil {
		return TypeAccess{allowAll: true, anyGrant: true}
	}
	p := e.policy.Load()
	if p == nil {
		return TypeAccess{allowAll: true, anyGrant: true}
	}

	grants := e.collectGrants(id, p)
	if len(grants) == 0 {
		return TypeAccess{}
	}

	access := TypeAccess{
		byPermission: make(map[string]map[string]bool, 2),
		anyGrant:     true,
	}

	mark := func(permission, resourceType string) {
		byType, ok := access.byPermission[permission]
		if !ok {
			byType = make(map[string]bool)
			access.byPermission[permission] = byType
		}
		byType[resourceType] = true
	}

	for _, g := range grants {
		for _, expr := range g.Resources {
			resType, ok := grantResourceType(expr)
			if !ok {
				continue
			}
			for _, permission := range g.Permissions {
				mark(permission, resType)
				for _, implied := range impliedTypes[resType] {
					mark(permission, implied)
				}
			}
		}
	}

	return access
}

// grantResourceType pulls the type prefix from a grant resource expression
// such as "service:web-*" → "service". A bare "*" is the wildcard type, which
// TypeAccess.Can resolves against every type.
func grantResourceType(expr string) (string, bool) {
	if expr == "*" {
		return "*", true
	}

	resType, _, ok := splitResource(expr)

	return resType, ok
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
