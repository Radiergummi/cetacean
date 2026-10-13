package cluster

import (
	"github.com/radiergummi/cetacean/internal/cache"
)

// CanReadEvent reports whether can admits the resource an event is about. The
// ACL evaluator resolves a resource's service and stack through the cache,
// which no longer holds a removed one, so a remove is also admitted by the
// service and stack it carries.
func CanReadEvent(ev cache.Event, can func(resource string) bool) bool {
	if can(string(ev.Type) + ":" + ev.Name) {
		return true
	}

	return ev.Service != "" && can("service:"+ev.Service) ||
		ev.Stack != "" && can("stack:"+ev.Stack)
}
