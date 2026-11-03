package cluster

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// PresentHistory names task entries as Swarm shows them and marks the
// resources an annotation names that are gone. cache.ExtractName must keep a
// task's ID as its name, since every read check is keyed on it, so this runs
// after filtering, for presentation only, through the caller's ReadableTaskNames.
func PresentHistory(
	c *cache.Cache,
	entries []cache.HistoryEntry,
	taskName func(e cache.HistoryEntry) string,
) []cache.HistoryEntry {
	named := make([]cache.HistoryEntry, len(entries))
	copy(named, entries)

	for i, e := range named {
		if e.Annotation != nil {
			a := c.WithRemovals(*e.Annotation)
			named[i].Resources, named[i].Annotation = a.Resources, &a
		}

		if e.Type != cache.EventTask {
			continue
		}

		named[i].Name = taskName(e)
	}

	return named
}

// ReadTimeline lists q for one caller: its text filters find a task by the
// name its row shows, and present names the entries returned from the same
// lookups. Present only what is returned; each name takes the cache's lock.
func ReadTimeline(
	c *cache.Cache,
	q cache.HistoryQuery,
	canRead func(resource string) bool,
) ([]cache.HistoryEntry, func([]cache.HistoryEntry) []cache.HistoryEntry) {
	q.TaskName = ReadableTaskNames(c, canRead)

	return c.Timeline(q), func(entries []cache.HistoryEntry) []cache.HistoryEntry {
		return PresentHistory(c, entries, q.TaskName)
	}
}

// ReadableTaskNames names a task entry for one caller as ReadableTaskName
// does, from the replica the entry recorded once Swarm has pruned the task. It
// remembers each name, since every lookup takes the cache's read lock and a
// restarting service repeats a few tasks. It is not safe for concurrent use.
func ReadableTaskNames(
	c *cache.Cache,
	canRead func(resource string) bool,
) func(e cache.HistoryEntry) string {
	names := make(map[string]string)

	return func(e cache.HistoryEntry) string {
		if name, ok := names[e.ResourceID]; ok {
			return name
		}

		task, ok := c.GetTask(e.ResourceID)
		if !ok {
			task = swarm.Task{
				ID: e.ResourceID, ServiceID: e.ServiceID, Slot: e.Slot, NodeID: e.NodeID,
			}
		}

		name := e.ResourceID
		if ok || e.Slot != 0 || e.NodeID != "" {
			name = readableTaskName(c, task, canRead)
		}

		names[e.ResourceID] = name

		return name
	}
}

// ReadableTaskName resolves one task ID to "<service>.<slot>", naming the
// service only where canRead admits it, since the name is the service's; it
// falls back to the ID otherwise or when either record has gone.
func ReadableTaskName(c *cache.Cache, taskID string, canRead func(resource string) bool) string {
	task, ok := c.GetTask(taskID)
	if !ok {
		return taskID
	}

	return readableTaskName(c, task, canRead)
}

// readableTaskName is ReadableTaskName for a task in hand.
func readableTaskName(c *cache.Cache, task swarm.Task, canRead func(resource string) bool) string {
	svc, ok := c.GetService(task.ServiceID)
	if !ok || !admits(canRead, "service:"+svc.Spec.Name) {
		return task.ID
	}

	return TaskName(task, &svc)
}

// HistoryReadable adapts canRead to a cache.HistoryQuery's Visible, keyed on
// the entry's type and the name it recorded — a task's is its ID. A gap names
// no resource, so whoever may read the timeline may read it.
func HistoryReadable(canRead func(resource string) bool) func(cache.HistoryEntry) bool {
	return func(e cache.HistoryEntry) bool {
		return e.Kind == cache.KindGap || canRead(string(e.Type)+":"+e.Name)
	}
}
