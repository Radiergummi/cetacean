package recommendations

import "slices"

// ACLResource is the resource a recommendation is authorized as: the service,
// node, network, secret or config it targets, or the swarm itself.
func (r Recommendation) ACLResource() string {
	switch r.Scope {
	case ScopeService:
		return "service:" + r.TargetName
	case ScopeNode:
		return "node:" + r.TargetName
	case ScopeSecret:
		return "secret:" + r.TargetName
	case ScopeConfig:
		return "config:" + r.TargetName
	case ScopeNetwork:
		return "network:" + r.TargetName
	default:
		return "swarm:cluster"
	}
}

// Readable keeps the recommendations whose target and every resource they
// reveal canRead allows. It may reuse recs, which every caller has copied from
// the engine.
func Readable(recs []Recommendation, canRead func(resource string) bool) []Recommendation {
	return slices.DeleteFunc(recs, func(rec Recommendation) bool {
		return !canRead(rec.ACLResource()) ||
			slices.ContainsFunc(rec.Reveals, func(r string) bool { return !canRead(r) })
	})
}
