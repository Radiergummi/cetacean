package mcp

import (
	"strings"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// subscriptionTarget is the one resource a subscribed URI names, by ID — or
// by name for stacks and volumes, which are keyed by it.
type subscriptionTarget struct {
	typ cache.EventType
	id  string
}

// subscription is a subscribed URI as dispatch sees it. A nil match is a URI
// no event updates, such as the cluster overview or a listing.
type subscription struct {
	target subscriptionTarget
	match  func(cache.Event) bool
}

func (sub subscription) matches(event cache.Event) bool {
	return sub.match != nil && sub.match(event)
}

// subscribableTypes is every resource type a cetacean://<type>s/<id> URI names.
var subscribableTypes = []cache.EventType{
	cache.EventNode, cache.EventService, cache.EventTask, cache.EventStack,
	cache.EventConfig, cache.EventSecret, cache.EventNetwork, cache.EventVolume,
}

// parseSubscriptionURI reads the resource a URI names, ignoring any
// sub-resource: a service's logs and compose change when the service does.
func parseSubscriptionURI(uri string) (subscriptionTarget, bool) {
	for _, typ := range subscribableTypes {
		rest, ok := strings.CutPrefix(uri, eventTypeToURIPrefix(typ))
		if !ok {
			continue
		}

		id, _, _ := strings.Cut(rest, "/")
		if id == "" {
			return subscriptionTarget{}, false
		}

		return subscriptionTarget{typ: typ, id: id}, true
	}

	return subscriptionTarget{}, false
}

// literalSubscription matches a URI by the identifier it names as written.
func literalSubscription(uri string, _ *auth.Identity) subscription {
	target, ok := parseSubscriptionURI(uri)
	if !ok {
		return subscription{}
	}

	return subscription{target: target, match: sse.ResourceMatcher(target.typ, target.id)}
}

// subscriptionFor resolves a subscribed URI and matches what the REST stream
// for the same resource sends: a service or node with its tasks, a stack with
// its members. A name the caller cannot read stays unresolved, as a read of
// it answers not found.
func (s *Server) subscriptionFor(uri string, identity *auth.Identity) subscription {
	target, ok := parseSubscriptionURI(uri)
	if !ok {
		return subscription{}
	}

	if id, resource, found := s.resolveTarget(
		identity,
		target,
	); found &&
		s.canRead(identity, resource) {
		target.id = id
	}

	if target.typ == cache.EventStack {
		return subscription{target: target, match: sse.StackMatcher(s.cache, target.id)}
	}

	return subscription{target: target, match: sse.ResourceMatcher(target.typ, target.id)}
}

// resolveTarget finds the resource a target names by ID or name, returning
// its ID and the ACL resource a read of it is checked against.
func (s *Server) resolveTarget(
	identity *auth.Identity,
	target subscriptionTarget,
) (string, string, bool) {
	prefix := string(target.typ) + ":"

	switch target.typ {
	case cache.EventService:
		svc, ok, _ := s.cache.ResolveService(target.id)
		return svc.ID, prefix + svc.Spec.Name, ok
	case cache.EventNode:
		node, ok, _ := s.cache.ResolveNode(target.id)
		return node.ID, prefix + cluster.NodeACLName(node), ok
	case cache.EventTask:
		task, ok, _ := s.resolveTaskAs(identity, target.id)
		return task.ID, prefix + task.ID, ok
	case cache.EventConfig:
		cfg, ok, _ := s.cache.ResolveConfig(target.id)
		return cfg.ID, prefix + cfg.Spec.Name, ok
	case cache.EventSecret:
		sec, ok, _ := s.cache.ResolveSecret(target.id)
		return sec.ID, prefix + sec.Spec.Name, ok
	case cache.EventNetwork:
		net, ok, _ := s.cache.ResolveNetwork(target.id)
		return net.ID, prefix + net.Name, ok
	case cache.EventStack:
		_, ok := s.cache.GetStack(target.id)
		return target.id, prefix + target.id, ok
	case cache.EventVolume:
		_, ok := s.cache.GetVolume(target.id)
		return target.id, prefix + target.id, ok
	default:
		return "", "", false
	}
}

// targetReadable reports whether identity may read the subscribed resource
// as it stands in the cache now; one that is gone is not.
func (s *Server) targetReadable(identity *auth.Identity, target subscriptionTarget) bool {
	_, resource, found := s.resolveTarget(identity, target)

	return found && s.canRead(identity, resource)
}
