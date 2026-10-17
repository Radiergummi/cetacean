package acl

import (
	"slices"
	"strings"
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

	// policyGeneration advances on every SetPolicy. The policy is hot-reloaded
	// from disk, so what an identity may see changes with no cache mutation
	// behind it, and anything keyed on the grants has to notice.
	policyGeneration atomic.Uint64
}

// PolicyGeneration returns a counter that advances whenever the policy is
// replaced.
func (e *Evaluator) PolicyGeneration() uint64 {
	if e == nil {
		return 0
	}

	return e.policyGeneration.Load()
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
	e.policyGeneration.Add(1)
}

// SetResolver sets the resource resolver for stack/task resolution.
func (e *Evaluator) SetResolver(r ResourceResolver) {
	if e == nil {
		return
	}
	e.resolver = r
}

// SetSource sets the provider-specific grant source. Grants are derived once
// per identity and evaluator, so it is set before the evaluator serves.
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
	grants, allowAll := e.grantsWith(id, permission)

	// Matched directly rather than through Checker: this runs for every event
	// sent to every subscriber, and a Checker allocates a closure.
	return allowAll || e.matches(grants, resource)
}

// Checker is Can with the identity's grants collected once, for a caller asking
// about many resources. It answers under the policy current when it was made.
func (e *Evaluator) Checker(id *auth.Identity, permission string) func(resource string) bool {
	grants, allowAll := e.grantsWith(id, permission)
	if allowAll {
		return func(string) bool { return true }
	}

	return func(resource string) bool { return e.matches(grants, resource) }
}

// grantsWith returns the identity's grants that carry permission, or allowAll
// for a nil evaluator or no policy loaded.
func (e *Evaluator) grantsWith(
	id *auth.Identity,
	permission string,
) (grants grantSet, allowAll bool) {
	if e == nil {
		return grantSet{}, true
	}
	p := e.policy.Load()
	if p == nil {
		return grantSet{}, true
	}

	if id == nil {
		return e.grantSetOf(id, p, permission), false
	}

	// Constant for one identity under one policy, and asked for again on every
	// event an SSE subscriber is sent.
	gs, _ := id.Derived(grantSetKey{e, p, permission}, func() any {
		return e.grantSetOf(id, p, permission)
	}).(grantSet)

	return gs, false
}

type grantSetKey struct {
	evaluator  *Evaluator
	policy     *Policy
	permission string
}

func (e *Evaluator) grantSetOf(id *auth.Identity, p *Policy, permission string) (grants grantSet) {
	for _, g := range e.collectGrants(id, p) {
		if !hasPermission(g, permission) {
			continue
		}
		grants.grants = append(grants.grants, g)
		for _, expr := range g.Resources {
			exprType, _, _ := strings.Cut(expr, ":")
			grants.services = grants.services || exprType == "service" || exprType == "*"
			grants.stacks = grants.stacks || exprType == "stack" || exprType == "*"
		}
	}
	return grants
}

// Filter returns only items the identity can access with the given permission.
//
// items is left untouched. A caller that owns the slice outright — one holding
// a copy the cache just handed it — and whose items are all one resource type
// should use FilterInPlaceNamed, which is the same walk without a second slice.
func Filter[T any](
	e *Evaluator,
	id *auth.Identity,
	permission string,
	items []T,
	resourceFunc func(T) string,
) []T {
	grants, allowAll := e.grantsWith(id, permission)
	if allowAll {
		return items
	}

	// Grown rather than sized for the whole input: a permissive policy pays
	// for the growth, but sizing for every item costs a restrictive one far
	// more — and a restrictive policy is the reason to run one at all.
	var result []T
	for _, item := range items {
		if e.matches(grants, resourceFunc(item)) {
			result = append(result, item)
		}
	}

	return result
}

// FilterInPlaceNamed is Filter for a caller that owns items outright and whose
// items are all one resource type, so each can be named rather than spelled as
// a "type:name" string the matcher immediately splits again. The survivors are
// written over items, which is reordered and must not be shared.
func FilterInPlaceNamed[T any](
	e *Evaluator,
	id *auth.Identity,
	permission string,
	items []T,
	resourceType string,
	nameFunc func(T) string,
) []T {
	grants, allowAll := e.grantsWith(id, permission)
	if allowAll {
		return items
	}

	result := items[:0]
	for _, item := range items {
		if e.matchesParts(grants, resourceType, nameFunc(item)) {
			result = append(result, item)
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

// CanReadEverything reports whether a grant gives the identity read on a bare
// "*". Typed patterns never count, even all of them together: they leave out
// anything without a type, which is what an endpoint not filtered per resource
// exposes.
func (e *Evaluator) CanReadEverything(id *auth.Identity) bool {
	if e == nil {
		return true
	}
	p := e.policy.Load()
	if p == nil {
		return true
	}

	for _, g := range e.collectGrants(id, p) {
		if hasPermission(g, "read") && slices.Contains(g.Resources, "*") {
			return true
		}
	}

	return false
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
		grants = append(grants, e.sourceGrants(id)...)
	}

	return grants
}

// grantSet is the grants carrying one permission, with whether any of them can
// name a service or a stack: the resolver is asked only when an answer could
// match, and once per resource rather than once per grant.
type grantSet struct {
	grants           []Grant
	services, stacks bool
}

// matches reports whether any grant covers the resource, including stack
// resolution and task inheritance.
func (e *Evaluator) matches(gs grantSet, resource string) bool {
	resType, resID, ok := splitResource(resource)
	if !ok {
		// Nothing to match a pattern against, but a bare wildcard still covers
		// it — see matchResource.
		return slices.ContainsFunc(gs.grants, func(g Grant) bool {
			return slices.Contains(g.Resources, "*")
		})
	}

	return e.matchesParts(gs, resType, resID)
}

// matchesParts is matches for a caller holding the two halves already, so that
// filtering a list need not build a "type:name" string per item for the direct
// comparison to split straight back apart.
func (e *Evaluator) matchesParts(gs grantSet, resType, resID string) bool {
	if anyCovers(gs.grants, resType, resID) {
		return true
	}

	if e.resolver == nil {
		return false
	}

	// Tasks inherit from their parent service, and through it from its stack;
	// a task has no stack of its own.
	if resType == "task" {
		if !gs.services && !gs.stacks {
			return false
		}

		svcName := e.resolver.ServiceOfTask(resID)
		if svcName == "" {
			return false
		}
		if gs.services && anyCovers(gs.grants, "service", svcName) {
			return true
		}
		if !gs.stacks {
			return false
		}

		stackName := e.resolver.StackOf("service", svcName)

		return stackName != "" && anyCovers(gs.grants, "stack", stackName)
	}

	if !gs.stacks {
		return false
	}

	stackName := e.resolver.StackOf(resType, resID)

	return stackName != "" && anyCovers(gs.grants, "stack", stackName)
}

func anyCovers(grants []Grant, resType, resName string) bool {
	return slices.ContainsFunc(grants, func(g Grant) bool {
		return grantCovers(g, resType, resName)
	})
}

// grantCovers reports whether any of the grant's resource patterns matches.
func grantCovers(g Grant, resType, resName string) bool {
	for _, expr := range g.Resources {
		if matchResourceParts(expr, resType, resName) {
			return true
		}
	}

	return false
}

// impliedTypes names, for a grant's resource type, the other types it covers —
// the type-level shadow of matchesParts' resolver walk, with
// cache.StackOf as the authority on the stack list. The expansion is
// unconditional where that walk is not, so it widens a listing, never a call.
var impliedTypes = map[string][]string{
	"stack":   {"service", "task", "config", "secret", "network", "volume"},
	"service": {"task"},
}

// TypeAccess is the type-level projection of an identity's grants: which
// resource *types* it may exercise a permission on, for callers filtering a
// catalog with no resource name to give matches. It answers "could
// this identity ever read a service?", never "may it read this one".
type TypeAccess struct {
	// granted is keyed by permission and resource type, already expanded, so
	// Can is a lookup rather than a rule. The "*" type means every type.
	granted map[typeKey]bool

	// allowAll mirrors Can's allow-all: a nil evaluator or no policy loaded.
	allowAll bool

	// anyGrant separates that from "policy loaded, this caller matched
	// nothing" — the two cases PermissionsFor's nil return conflates.
	anyGrant bool
}

type typeKey struct{ permission, resourceType string }

// AllowAll reports that no policy is in force, so every type is permitted.
func (t TypeAccess) AllowAll() bool { return t.allowAll }

// HasAnyGrant reports whether the identity matched at least one grant.
func (t TypeAccess) HasAnyGrant() bool { return t.anyGrant }

// Can reports whether the identity may exercise permission on some resource of
// resourceType.
func (t TypeAccess) Can(permission, resourceType string) bool {
	if t.allowAll {
		return true
	}

	return t.granted[typeKey{permission, resourceType}] ||
		t.granted[typeKey{permission, "*"}]
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

	access := TypeAccess{granted: make(map[typeKey]bool), anyGrant: true}
	for _, g := range grants {
		for _, expr := range g.Resources {
			resType, ok := grantResourceType(expr)
			if !ok {
				continue
			}

			// hasPermission owns "write implies read", so the projection is
			// stored already expanded and Can needs no rule of its own.
			for permission := range validPermissions {
				if !hasPermission(g, permission) {
					continue
				}

				access.granted[typeKey{permission, resType}] = true
				for _, implied := range impliedTypes[resType] {
					access.granted[typeKey{permission, implied}] = true
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
	return strings.Cut(resource, ":")
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
