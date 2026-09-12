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

	// Label evaluation: check resource labels first when enabled.
	labelled := false
	if e.labelsEnabled && e.resolver != nil {
		allowed, handled, hasLabels := e.checkLabels(id, permission, resource)
		if handled {
			return allowed
		}
		labelled = hasLabels
	}

	// An absent policy means allow-all, except on a resource carrying ACL
	// labels: there the labels *are* the policy, and allow-all would hand it to
	// the audiences the label leaves out. Scoping the suppression to labelled
	// resources is what keeps acl.labels from denying an unpolicied cluster.
	if p == nil {
		if !labelled {
			return true
		}
		p = &Policy{}
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
	policyAbsent := p == nil
	if policyAbsent {
		if !e.labelsEnabled {
			return items
		}
		p = &Policy{}
	}

	grants := e.collectGrants(id, p)

	// One bulk label read per type touched, memoised for the call. Resolving
	// per item costs the resolver a scan each time, which is quadratic over a
	// page — see acl.ResourceResolver.LabelsByType.
	byType := map[string]map[string]map[string]string{}
	labelsFor := func(resource string) map[string]string {
		resType, resName, ok := splitResource(resource)
		if !ok {
			return nil
		}
		if resType == "task" {
			resName = e.resolver.ServiceOfTask(resName)
			if resName == "" {
				return nil
			}
			resType = "service"
		}

		known, cached := byType[resType]
		if !cached {
			known = e.resolver.LabelsByType(resType)
			byType[resType] = known
		}
		return known[resName]
	}

	var result []T
	for _, item := range items {
		resource := resourceFunc(item)

		// Check labels first when enabled.
		if e.labelsEnabled && e.resolver != nil {
			allowed, handled, labelled := decideFromLabels(
				labelsFor(resource),
				id,
				permission,
				resource,
			)
			if handled {
				if allowed {
					result = append(result, item)
				}
				continue
			}

			// Carries no labels and there is no policy to fall through to, so
			// it sits outside the label mechanism entirely and keeps the
			// allow-all an absent policy has always meant. Mirrors Can.
			if policyAbsent && !labelled {
				result = append(result, item)
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

// checkLabels evaluates label-based ACL. handled means the label result is
// authoritative for this resource+identity; otherwise the caller falls through
// to config grants. labelled reports whether the resource carried ACL labels at
// all, which decides whether an absent policy still means allow-all for it.
func (e *Evaluator) checkLabels(
	id *auth.Identity,
	permission string,
	resource string,
) (allowed, handled, labelled bool) {
	return decideFromLabels(e.resolveLabels(resource), id, permission, resource)
}

// decideFromLabels is checkLabels once the labels are in hand. Can resolves one
// resource; Filter reads a whole type at once and calls this per item, so the
// rule they apply is the same one.
func decideFromLabels(
	labels map[string]string,
	id *auth.Identity,
	permission string,
	resource string,
) (allowed, handled, labelled bool) {
	if labels == nil || !hasACLLabels(labels) {
		return false, false, false
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
			return effectiveWrite, true, true
		case "read":
			return effectiveRead, true, true
		default:
			return false, true, true
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
	return false, false, true
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

// impliedTypes names, for a grant's resource type, the other resource types a
// grant on it also covers — the type-level shadow of grantMatchesResource's
// resolver walk. A stack grant reaches every resource StackOf can place in a
// stack; a service grant reaches that service's tasks. Nothing reaches nodes,
// plugins or the swarm itself, which belong to no stack.
//
// cache.StackOf's switch is the authority on the stack list. Two tests hold
// the rules together: TestTypeGrantsAgreesWithCan drives the real evaluator
// over a resolver holding one resource of each type and fails if this
// projection and Can disagree in either direction, and
// TestImpliedStackTypesMatchTheResolver drives the real Cache, so adding a
// case to StackOf without adding it here fails rather than silently hiding
// the new type from every listing.
//
// The expansion is unconditional where grantMatchesResource's is not — that
// walk only happens when a resolver is attached. With no resolver, TypeGrants
// still reports a stack grant as reaching services while Can would not. That
// widens a listing, never a call, which is the direction this projection is
// already allowed to err in.
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
