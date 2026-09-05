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
