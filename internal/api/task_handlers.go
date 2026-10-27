package api

import (
	"context"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Tasks ---

// EnrichedTask is an alias for cluster.EnrichedTask, kept for the REST
// response and test imports that referenced the api-local name historically.
type EnrichedTask = cluster.EnrichedTask

// enrichedTaskID extracts the JSON-LD @id for an enriched task. Shared across
// task list handlers and service/node task sub-collections.
func enrichedTaskID(t EnrichedTask) string { return "/tasks/" + t.ID }

// taskStateSortKey returns a sort key that orders running tasks first,
// then starting/preparing, then terminal states alphabetically.
func taskStateSortKey(state swarm.TaskState) string {
	switch state {
	case swarm.TaskStateRunning:
		return "0"
	case swarm.TaskStateStarting:
		return "1"
	case swarm.TaskStatePreparing:
		return "1"
	case swarm.TaskStateReady:
		return "1"
	case swarm.TaskStateNew:
		return "1"
	default:
		return "2" + string(state)
	}
}

func (h *Handlers) HandleListTasks(w http.ResponseWriter, r *http.Request) {
	spec := listSpec[swarm.Task]{
		resourceType: "task",
		linkTemplate: "/tasks/{id}",
		list:         h.cache.ListTasks,
		aclName:      func(t swarm.Task) string { return t.ID },
		filterEnv:    filter.Tasks,
		sortKeys: map[string]func(swarm.Task) string{
			"state":   func(t swarm.Task) string { return taskStateSortKey(t.Status.State) },
			"service": func(t swarm.Task) string { return t.ServiceID },
			"node":    func(t swarm.Task) string { return t.NodeID },
		},
	}

	// Enrichment reads the same cache, so the derived validator covers it.
	validator := h.derivedETag(r)
	if listNotModified(h, w, r, spec, validator) {
		return
	}

	r, validator = h.ifRange(r, validator)

	tasks, _, p, ok := prepareList(h, w, r, spec, cluster.VersionSelection{})
	if !ok {
		return
	}

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		canRead := h.readChecker(r)
		writeListCSV(w, r, "task", tasks, p, func(page []swarm.Task) []cluster.Row {
			return h.enrichedTaskRows(cluster.EnrichTasks(h.cache, page, canRead))
		})
		return
	}

	writeLinkTemplate(w, r, spec.linkTemplate)
	writeListPage(
		w,
		r,
		tasks,
		p,
		validator,
		"Task",
		enrichedTaskID,
		func(page []swarm.Task) []cluster.EnrichedTask {
			return cluster.EnrichTasks(h.cache, page, h.readChecker(r))
		},
	)
}

// readChecker is the caller's read predicate, for naming a resource's parents.
func (h *Handlers) readChecker(r *http.Request) func(resource string) bool {
	return h.acl.Checker(auth.IdentityFromContext(r.Context()), "read")
}

// enrichedTaskRows looks up each task's parent rather than handing the builder
// every service, which would index the whole service table per page. A parent
// enrichment left unnamed is one the caller cannot read, so it stays out.
func (h *Handlers) enrichedTaskRows(tasks []cluster.EnrichedTask) []cluster.Row {
	parents := make([]swarm.Service, 0, len(tasks))
	for _, task := range tasks {
		if task.ServiceName == "" {
			continue
		}

		if svc, ok := h.cache.GetService(task.ServiceID); ok {
			parents = append(parents, svc)
		}
	}

	return cluster.RowsForTasks(tasks, parents)
}

// writeTaskCSV names the file after the parent, so two downloads do not
// collide.
func (h *Handlers) writeTaskCSV(
	w http.ResponseWriter,
	r *http.Request,
	parent string,
	tasks []cluster.EnrichedTask,
) {
	writeCSV(w, r, "tasks-"+parent, csvTableForRows("task", h.enrichedTaskRows(tasks)))
}

func (h *Handlers) HandleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := lookupACL(h, w, r, "task", id, h.cache.GetTask, func(t swarm.Task) string {
		return "task:" + t.ID
	})
	if !ok {
		return
	}
	// The grant DELETE is gated on, which is the parent service's.
	h.setAllowSubResource(w, r, h.taskServiceResource(r))

	rep, ok := representationOr404(w, r, "task", id, h.taskRepresentation)
	if !ok {
		return
	}

	writeCachedJSON(w, r, rep)
}

func (h *Handlers) HandleTaskLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := lookupACL(h, w, r, "task", id, h.cache.GetTask, func(t swarm.Task) string {
		return "task:" + t.ID
	}); !ok {
		return
	}
	h.serveLogs(
		w,
		r,
		func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error) {
			return h.dockerClient.Logs(ctx, docker.TaskLog, id, tail, follow, since, until)
		},
	)
}
