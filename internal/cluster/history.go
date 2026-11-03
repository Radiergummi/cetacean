package cluster

import "github.com/radiergummi/cetacean/internal/cache"

// PresentHistory names task entries as Swarm shows them and marks the
// resources an annotation names that are gone. cache.ExtractName must keep a
// task's ID as its name, since every read check is keyed on it, so this runs
// after filtering, for presentation only. Naming is best-effort.
func PresentHistory(
	c *cache.Cache,
	entries []cache.HistoryEntry,
	canRead func(resource string) bool,
) []cache.HistoryEntry {
	named := make([]cache.HistoryEntry, len(entries))
	copy(named, entries)

	taskName := ReadableTaskNames(c, canRead)

	for i, e := range named {
		if e.Annotation != nil {
			a := c.WithRemovals(*e.Annotation)
			named[i].Resources, named[i].Annotation = a.Resources, &a
		}

		if e.Type != cache.EventTask {
			continue
		}

		named[i].Name = taskName(e.ResourceID)
	}

	return named
}

// ReadableTaskNames is ReadableTaskName for one caller, remembering each name:
// a restarting service produces runs of events for the same few tasks, and
// each lookup takes the cache's read lock. It is not safe for concurrent use.
func ReadableTaskNames(
	c *cache.Cache,
	canRead func(resource string) bool,
) func(taskID string) string {
	names := make(map[string]string)

	return func(taskID string) string {
		name, ok := names[taskID]
		if !ok {
			name = ReadableTaskName(c, taskID, canRead)
			names[taskID] = name
		}

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

	svc, ok := c.GetService(task.ServiceID)
	if !ok || !admits(canRead, "service:"+svc.Spec.Name) {
		return taskID
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
