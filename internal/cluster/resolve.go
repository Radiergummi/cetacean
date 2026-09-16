package cluster

import (
	"strings"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// ResolveTask returns the task with this ID or rendered name. It cannot live
// in the cache beside the other seven: the name is derived here, and this
// package imports the cache.
func ResolveTask(c *cache.Cache, identifier string) (swarm.Task, bool, error) {
	if task, ok := c.GetTask(identifier); ok {
		return task, true, nil
	}

	// Cut at the *last* separator: Docker permits a dot in a service name,
	// while neither half of the suffix TaskName appends can hold one. No
	// separator at all is not a miss either — an unassigned global task
	// renders as the bare service name — so the scan below decides.
	serviceName := identifier
	if dot := strings.LastIndex(identifier, "."); dot >= 0 {
		serviceName = identifier[:dot]
	}

	service, ok, err := c.ResolveService(serviceName)
	if err != nil || !ok {
		return swarm.Task{}, false, err
	}

	// Swarm keeps a record for every replica it has replaced, rendering under
	// the name of the one that succeeded it. A caller means the live one, and
	// where none is live there is nothing to name.
	for _, task := range c.ListTasksByService(service.ID) {
		if TaskIsLive(task) && TaskName(task, &service) == identifier {
			return task, true, nil
		}
	}

	return swarm.Task{}, false, nil
}
