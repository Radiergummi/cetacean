package mcp

import (
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// subscriptionTarget is the one resource a subscribed URI names, by ID or by
// name as written. It doubles as the subscription index's key: an event looks
// up every target that could name what it changed.
type subscriptionTarget struct {
	typ cache.EventType
	id  string
}

// subscription is a subscribed URI as dispatch sees it. A zero target is a
// URI no event updates, such as the cluster overview or a listing.
type subscription struct {
	target subscriptionTarget

	// resolved is the ID a prefix resolved to at subscribe time. A name needs
	// none: events carry the name, so it keeps matching whatever holds it.
	resolved string
}

// keys is every index key the subscription is found under.
func (sub subscription) keys() []subscriptionTarget {
	if sub.target.id == "" {
		return nil
	}

	keys := []subscriptionTarget{sub.target}
	if sub.resolved != "" && sub.resolved != sub.target.id {
		keys = append(keys, subscriptionTarget{typ: sub.target.typ, id: sub.resolved})
	}

	return keys
}

// eventKey is an index key an event looks up. via is the ACL resource the
// subscriber must also read, when the event reaches it through a parent;
// ownTarget asks instead that the subscriber may read what it subscribed to.
type eventKey struct {
	target    subscriptionTarget
	via       string
	ownTarget bool
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
	target, _ := parseSubscriptionURI(uri)

	return subscription{target: target}
}

// directEventKeys are the keys naming the changed resource itself.
func directEventKeys(event cache.Event) []eventKey {
	if !isResourceEvent(event.Type) {
		return nil
	}

	keys := []eventKey{{target: subscriptionTarget{typ: event.Type, id: event.ID}}}
	if event.Name != "" && event.Name != event.ID {
		keys = append(keys, eventKey{target: subscriptionTarget{typ: event.Type, id: event.Name}})
	}

	return keys
}

// subscriptionFor resolves an ID prefix the caller can read; anything else is
// matched as written, against the IDs and names events carry.
func (s *Server) subscriptionFor(uri string, identity *auth.Identity) subscription {
	target, ok := parseSubscriptionURI(uri)
	if !ok {
		return subscription{}
	}

	sub := subscription{target: target}
	if id, resource, found := s.resolveTarget(identity, target); found &&
		s.canRead(identity, resource) {
		sub.resolved = id
	}

	return sub
}

// eventKeys is every key an event reaches, as the REST stream for the same
// resource sees it: a service or node with its tasks, a task by its slot name,
// a stack with its members, a network with what moves its address figure.
func (s *Server) eventKeys(event cache.Event) []eventKey {
	keys := append(directEventKeys(event), s.addressNetworkKeys(event)...)

	switch event.Type {
	case cache.EventTask:
		task, ok := event.Resource.(swarm.Task)
		if !ok {
			return keys
		}

		return append(keys, s.taskParentKeys(event, task)...)

	case cache.EventService, cache.EventConfig, cache.EventSecret,
		cache.EventNetwork, cache.EventVolume:
		if stack := s.eventStack(event); stack != "" {
			keys = append(keys, stackKey(stack))
		}

	case cache.EventNode, cache.EventStack, cache.EventSync:
	}

	return keys
}

// taskParentKeys reaches a task's service, its slot name and stack, and its
// node. Each is found by its ID and its name, so a parent recreated under the
// same name is still the one subscribed to.
func (s *Server) taskParentKeys(event cache.Event, task swarm.Task) []eventKey {
	var keys []eventKey

	service := func(id string) subscriptionTarget {
		return subscriptionTarget{typ: cache.EventService, id: id}
	}

	if svc, ok := s.cache.GetService(task.ServiceID); ok {
		via := "service:" + svc.Spec.Name
		keys = append(keys,
			eventKey{target: service(svc.ID), via: via},
			eventKey{target: service(svc.Spec.Name), via: via},
			eventKey{
				target: subscriptionTarget{
					typ: cache.EventTask,
					id:  cluster.TaskName(task, &svc),
				},
				via: via,
			},
		)

		if stack := svc.Spec.Labels[cache.StackLabel]; stack != "" {
			keys = append(keys, stackKey(stack))
		}
	} else {
		keys = append(keys, eventKey{target: service(task.ServiceID)})
		if event.Service != "" {
			via := "service:" + event.Service
			keys = append(keys, eventKey{target: service(event.Service), via: via})
		}

		if event.Stack != "" {
			keys = append(keys, stackKey(event.Stack))
		}
	}

	if task.NodeID == "" {
		return keys
	}

	node := func(id string) subscriptionTarget {
		return subscriptionTarget{typ: cache.EventNode, id: id}
	}

	if n, ok := s.cache.GetNode(task.NodeID); ok {
		via := "node:" + cluster.NodeACLName(n)
		keys = append(keys, eventKey{target: node(n.ID), via: via})
		if n.Description.Hostname != "" {
			keys = append(keys, eventKey{target: node(n.Description.Hostname), via: via})
		}
	} else {
		keys = append(keys, eventKey{target: node(task.NodeID)})
	}

	return keys
}

// addressNetworkKeys reaches the watched networks whose address figure the
// event may move. Nothing is worked out while no network is watched, which is
// the common case on a path every cache write takes.
func (s *Server) addressNetworkKeys(event cache.Event) []eventKey {
	watched := s.notifications.networkTargets()
	if len(watched) == 0 {
		return nil
	}

	addressNetworks := s.addressNetworks
	if addressNetworks == nil {
		addressNetworks = cluster.AddressNetworks
	}

	var keys []eventKey

	for _, id := range addressNetworks(s.cache, event) {
		keys = append(keys, eventKey{
			target:    subscriptionTarget{typ: cache.EventNetwork, id: id},
			ownTarget: true,
		})

		if net, ok := s.cache.GetNetwork(id); ok && net.Name != "" {
			keys = append(keys, eventKey{
				target:    subscriptionTarget{typ: cache.EventNetwork, id: net.Name},
				ownTarget: true,
			})
		}
	}

	return keys
}

func stackKey(name string) eventKey {
	return eventKey{
		target: subscriptionTarget{typ: cache.EventStack, id: name},
		via:    "stack:" + name,
	}
}

// eventStack names the stack a changed resource belongs to, by the label
// stacks are derived from. A cross-reference event carries no resource, so
// that one is read from the cache.
func (s *Server) eventStack(event cache.Event) string {
	if event.Stack != "" {
		return event.Stack
	}

	resource := event.Resource
	if resource == nil {
		resource = s.cachedResource(event.Type, event.ID)
	}

	switch r := resource.(type) {
	case swarm.Service:
		return r.Spec.Labels[cache.StackLabel]
	case swarm.Config:
		return r.Spec.Labels[cache.StackLabel]
	case swarm.Secret:
		return r.Spec.Labels[cache.StackLabel]
	case network.Summary:
		return r.Labels[cache.StackLabel]
	case volume.Volume:
		return r.Labels[cache.StackLabel]
	default:
		return ""
	}
}

func (s *Server) cachedResource(typ cache.EventType, id string) any {
	var (
		resource any
		ok       bool
	)

	switch typ {
	case cache.EventService:
		resource, ok = s.cache.GetService(id)
	case cache.EventConfig:
		resource, ok = s.cache.GetConfig(id)
	case cache.EventSecret:
		resource, ok = s.cache.GetSecret(id)
	case cache.EventNetwork:
		resource, ok = s.cache.GetNetwork(id)
	case cache.EventVolume:
		resource, ok = s.cache.GetVolume(id)
	case cache.EventNode, cache.EventTask, cache.EventStack, cache.EventSync:
	}

	if !ok {
		return nil
	}

	return resource
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
