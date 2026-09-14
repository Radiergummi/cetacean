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
	if e == nil {
		return items
	}
	p := e.policy.Load()
	policyAbsent := p == nil
	if policyAbsent {
		// The same condition the label path is gated on. Testing only the flag
		// dropped every item where Can allows every item, on a deployment with
		// labels on and no resolver set.
		if !e.labelsEnabled || e.resolver == nil {
			return items
		}
		p = &Policy{}
	}

	grants := e.collectGrants(id, p)
	labelsFor := e.labelLookup()

	// Grown rather than sized for the whole input: a permissive policy pays
	// for the growth, but sizing for every item costs a restrictive one far
	// more — and a restrictive policy is the reason to run one at all.
	var result []T
	for _, item := range items {
		resource := resourceFunc(item)
		resType, resName, ok := splitResource(resource)
		if ok {
			if keep, decided := e.labelDecision(
				id, permission, resType, resName, labelsFor, policyAbsent,
			); decided {
				if keep {
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
	if e == nil {
		return items
	}
	p := e.policy.Load()
	policyAbsent := p == nil
	if policyAbsent {
		if !e.labelsEnabled || e.resolver == nil {
			return items
		}
		p = &Policy{}
	}

	grants := e.collectGrants(id, p)
	labelsFor := e.labelLookup()

	result := items[:0]
	for _, item := range items {
		name := nameFunc(item)

		if keep, decided := e.labelDecision(
			id, permission, resourceType, name, labelsFor, policyAbsent,
		); decided {
			if keep {
				result = append(result, item)
			}

			continue
		}

		for _, g := range grants {
			if hasPermission(g, permission) && e.grantMatchesParts(g, resourceType, name) {
				result = append(result, item)
				break
			}
		}
	}

	return result
}

// labelLookup reads a whole type at once and memoises it for the call.
// Resolving per item costs the resolver a scan each time, which is quadratic
// over a page — see acl.ResourceResolver.LabelsByType.
func (e *Evaluator) labelLookup() func(resType, resName string) map[string]string {
	byType := map[string]map[string]map[string]string{}

	return func(resType, resName string) map[string]string {
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
}

// labelDecision is the per-item half of the label rules, shared by both
// filters so a list cannot apply a different rule from a detail read. decided
// reports that the labels settled the question; keep is their answer.
func (e *Evaluator) labelDecision(
	id *auth.Identity,
	permission, resType, resName string,
	labelsFor func(resType, resName string) map[string]string,
	policyAbsent bool,
) (keep, decided bool) {
	if !e.labelsEnabled || e.resolver == nil {
		return false, false
	}

	allowed, handled, labelled := decideFromLabels(
		labelsFor(resType, resName), id, permission, resType+":"+resName,
	)
	if handled {
		return allowed, true
	}

	// Carries no labels and there is no policy to fall through to, so it sits
	// outside the label mechanism entirely and keeps the allow-all an absent
	// policy has always meant. Mirrors Can.
	if policyAbsent && !labelled {
		return true, true
	}

	return false, false
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

	// A label-only identity holds no policy grant and still has access, so
	// labels have to be asked. Asking whether they are *enabled* answered yes
	// for everyone, which handed cluster-wide metrics to identities no label
	// names — these endpoints have no per-resource filter behind them.
	return len(grants) > 0 || e.hasAnyLabelGrant(id)
}

// labelledTypes are the resource types a cetacean.acl.* label can appear on.
// Tasks are absent because a task inherits its service's labels rather than
// carrying its own.
var labelledTypes = []string{"service", "config", "secret", "network", "volume", "node"}

// hasAnyLabelGrant reports whether any labelled resource names this identity.
// It stops at the first match, so the common case — an identity that does hold
// a grant — never reaches it, and one that does not usually stops early.
func (e *Evaluator) hasAnyLabelGrant(id *auth.Identity) bool {
	if !e.labelsEnabled || e.resolver == nil || id == nil {
		return false
	}

	for _, resType := range labelledTypes {
		for name, labels := range e.resolver.LabelsByType(resType) {
			if allowed, handled, _ := decideFromLabels(
				labels, id, "read", resType+":"+name,
			); handled && allowed {
				return true
			}
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
	resType, resID, ok := splitResource(resource)
	if !ok {
		// Nothing to match a pattern against, but a bare wildcard still covers
		// it — see matchResource.
		return slices.Contains(g.Resources, "*")
	}

	return e.grantMatchesParts(g, resType, resID)
}

// grantMatchesParts is grantMatchesResource for a caller holding the two halves
// already, so that filtering a list need not build a "type:name" string per
// item for the direct comparison to split straight back apart.
func (e *Evaluator) grantMatchesParts(g Grant, resType, resID string) bool {
	if grantCovers(g, resType, resID) {
		return true
	}

	// Stack resolution: if no direct match, check if the resource belongs
	// to a stack that a grant covers.
	if e.resolver == nil {
		return false
	}

	// Task inheritance: tasks inherit from their parent service.
	if resType == "task" {
		if svcName := e.resolver.ServiceOfTask(resID); svcName != "" {
			if grantCovers(g, "service", svcName) {
				return true
			}
			// Also check the parent service's stack (task→service→stack).
			if stackName := e.resolver.StackOf("service", svcName); stackName != "" {
				if grantCovers(g, "stack", stackName) {
					return true
				}
			}
		}
	}

	// Stack membership: check if the resource belongs to a matching stack.
	if stackName := e.resolver.StackOf(resType, resID); stackName != "" {
		return grantCovers(g, "stack", stackName)
	}

	return false
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

	// A label-only identity projects to no types from the policy alone, and
	// MCP hides every tool it could in fact call. The labels decide which
	// types it reaches, so they are projected alongside the grants.
	labelled := e.labelTypeGrants(id)
	if len(grants) == 0 && len(labelled) == 0 {
		return TypeAccess{}
	}

	access := TypeAccess{granted: make(map[typeKey]bool), anyGrant: true}
	for key := range labelled {
		access.granted[key] = true
	}
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

// labelTypeGrants projects the labels naming this identity onto the types they
// grant. One walk per type, stopping at the first resource that matches: the
// answer is type-level, so a second match adds nothing.
func (e *Evaluator) labelTypeGrants(id *auth.Identity) map[typeKey]bool {
	if !e.labelsEnabled || e.resolver == nil || id == nil {
		return nil
	}

	granted := map[typeKey]bool{}
	for _, resType := range labelledTypes {
		for name, labels := range e.resolver.LabelsByType(resType) {
			resource := resType + ":" + name
			for permission := range validPermissions {
				if granted[typeKey{permission, resType}] {
					continue
				}
				if allowed, handled, _ := decideFromLabels(
					labels, id, permission, resource,
				); handled && allowed {
					granted[typeKey{permission, resType}] = true
					for _, implied := range impliedTypes[resType] {
						granted[typeKey{permission, implied}] = true
					}
				}
			}
		}
	}

	return granted
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
