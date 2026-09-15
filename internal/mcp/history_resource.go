package mcp

import (
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// nameHistoryTasks replaces the name on task history entries with the one Swarm
// shows. cache.ExtractName must keep recording a task's ID as its name, since
// every read-side permission check is keyed on it — so this runs after
// filtering, for presentation only. Naming is best-effort and falls back.
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
