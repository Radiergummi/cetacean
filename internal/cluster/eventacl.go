package cluster

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// CanReadEvent reports whether can admits the resource an event is about. The
// ACL evaluator finds a task's service through the cache, which no longer holds
// a removed task, so a remove is also admitted by the service the task carries.
func CanReadEvent(c *cache.Cache, ev cache.Event, can func(resource string) bool) bool {
	if can(string(ev.Type) + ":" + ev.Name) {
		return true
	}

	task, ok := ev.Resource.(swarm.Task)
	if !ok || ev.Action != "remove" {
		return false
	}

	svc, ok := c.GetService(task.ServiceID)

	return ok && can("service:"+svc.Spec.Name)
}
