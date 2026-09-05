package mcp

import (
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// nameHistoryTasks replaces the name on task history entries with the one
// Swarm shows — "<service>.<slot>" — leaving every other type untouched.
//
// cache.ExtractName records a task's own ID as its name, and it must keep
// doing so: the ACL resolver looks a task's parent service up by task ID
// (acl.ResourceSource.ServiceOfTask), so the ID is the key every read-side
// permission check is built on, including filterHistory's. The correction is
// therefore made after filtering and for presentation only — resourceId still
// carries the ID, which is what addresses the task.
//
// Without this, a history read of a restarting service is a wall of opaque
// IDs: the resource that exists to answer "what changed?" cannot say what
// changed. Naming is best-effort, since a task can outlive its service record
// or arrive before it; cluster.TaskName falls back to the ID.
func nameHistoryTasks(c *cache.Cache, entries []cache.HistoryEntry) []cache.HistoryEntry {
	named := make([]cache.HistoryEntry, len(entries))
	copy(named, entries)

	// A restarting service produces runs of events for the same few tasks, and
	// each lookup takes the cache's read lock, so resolved names are reused.
	names := make(map[string]string)

	for i, e := range named {
		if e.Type != cache.EventTask {
			continue
		}

		name, ok := names[e.ResourceID]
		if !ok {
			name = taskDisplayName(c, e.ResourceID)
			names[e.ResourceID] = name
		}

		named[i].Name = name
	}

	return named
}

// taskDisplayName resolves one task ID to "<service>.<slot>", falling back to
// the ID when either record has already gone.
func taskDisplayName(c *cache.Cache, taskID string) string {
	task, ok := c.GetTask(taskID)
	if !ok {
		return taskID
	}

	svc, ok := c.GetService(task.ServiceID)
	if !ok {
		return taskID
	}

	return cluster.TaskName(task, &svc)
}
