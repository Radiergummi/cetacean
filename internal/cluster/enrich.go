package cluster

import (
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
