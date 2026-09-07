package cluster

import (
	"fmt"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// EnrichedTask extends swarm.Task with resolved service and node names.
type EnrichedTask struct {
	swarm.Task
	ServiceName  string `json:"ServiceName,omitempty"`
	NodeHostname string `json:"NodeHostname,omitempty"`
}

// EnrichTask resolves service name and node hostname for a task.
func EnrichTask(c *cache.Cache, t swarm.Task) EnrichedTask {
	et := EnrichedTask{Task: t}
	if svc, ok := c.GetService(t.ServiceID); ok {
		et.ServiceName = svc.Spec.Name
	}
	if node, ok := c.GetNode(t.NodeID); ok {
		et.NodeHostname = node.Description.Hostname
	}
	return et
}

// EnrichTasks enriches a slice of tasks.
func EnrichTasks(c *cache.Cache, tasks []swarm.Task) []EnrichedTask {
	out := make([]EnrichedTask, len(tasks))
	for i, t := range tasks {
		out[i] = EnrichTask(c, t)
	}
	return out
}

// EnrichTasksWithin names each task's parents from the slices given rather than
// from the cache, leaving a name empty when the parent is not among them.
//
// It exists so a caller that must not disclose every parent name can pass only
// the subset it may: internal/mcp hands it the ACL-filtered services and nodes,
// so an enriched task never carries a name from behind the caller's grants —
// the rule TaskDigest and RowsForTasks already follow. The task's own record
// still carries the parent IDs, so nothing the caller could otherwise reach is
// withheld.
//
// It is also the cheaper shape at cluster scale: EnrichTasks takes two cache
// read locks per task, this one builds two maps and takes none.
func EnrichTasksWithin(
	tasks []swarm.Task,
	services []swarm.Service,
	nodes []swarm.Node,
) []EnrichedTask {
	serviceNames := make(map[string]string, len(services))
	for _, svc := range services {
		serviceNames[svc.ID] = svc.Spec.Name
	}

	nodeNames := make(map[string]string, len(nodes))
	for _, node := range nodes {
		nodeNames[node.ID] = node.Description.Hostname
	}

	out := make([]EnrichedTask, len(tasks))
	for i, t := range tasks {
		out[i] = EnrichedTask{
			Task:         t,
			ServiceName:  serviceNames[t.ServiceID],
			NodeHostname: nodeNames[t.NodeID],
		}
	}

	return out
}

// TaskName is what to call a task, following Docker's own convention: a
// replicated task is "<service>.<slot>", a global one "<service>.<node>" since
// a global service has no slot to distinguish its replicas by.
//
// It is shared rather than inlined because a task's name was being derived
// three different ways in this package — a list row named a task after its
// service alone, so every replica of one service rendered identically, while
// the digest and the search results named the same records "<service>.<slot>".
// One tool reporting two names for one record is exactly the drift this
// package exists to prevent, so the rule lives here and has one caller each.
//
// service is a pointer because a caller may hold a task whose parent it cannot
// read; a nil service falls back to the task's own ID, which is always present.
func TaskName(task swarm.Task, service *swarm.Service) string {
	if service == nil {
		return task.ID
	}

	switch {
	case service.Spec.Mode.Global != nil && task.NodeID != "":
		return service.Spec.Name + "." + task.NodeID

	case service.Spec.Mode.Global != nil:
		// An unassigned global task has no node yet; "<service>." with a
		// trailing dot would read as a truncated name.
		return service.Spec.Name

	default:
		return fmt.Sprintf("%s.%d", service.Spec.Name, task.Slot)
	}
}

// RedactSecret returns a copy of the secret with Spec.Data set to nil.
func RedactSecret(s swarm.Secret) swarm.Secret {
	s.Spec.Data = nil
	return s
}

// RedactSecrets returns a new slice of secrets with all Data fields nilled.
func RedactSecrets(secrets []swarm.Secret) []swarm.Secret {
	out := make([]swarm.Secret, len(secrets))
	for i, s := range secrets {
		out[i] = RedactSecret(s)
	}
	return out
}
