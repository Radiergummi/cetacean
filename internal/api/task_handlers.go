package api

import (
	"context"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Tasks ---

type EnrichedTask struct {
	swarm.Task
	ServiceName  string `json:"ServiceName,omitempty"`
	NodeHostname string `json:"NodeHostname,omitempty"`
}

func (h *Handlers) enrichTask(t swarm.Task) EnrichedTask {
	et := EnrichedTask{Task: t}
	if svc, ok := h.cache.GetService(t.ServiceID); ok {
		et.ServiceName = svc.Spec.Name
	}
	if node, ok := h.cache.GetNode(t.NodeID); ok {
		et.NodeHostname = node.Description.Hostname
	}
	return et
}

func (h *Handlers) enrichTasks(tasks []swarm.Task) []EnrichedTask {
	out := make([]EnrichedTask, len(tasks))
	for i, t := range tasks {
		out[i] = h.enrichTask(t)
	}
	return out
}

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
	tasks, p, ok := prepareList(h, w, r, listSpec[swarm.Task]{
		resourceType: "task",
		linkTemplate: "/tasks/{id}",
		list:         h.cache.ListTasks,
		aclResource:  func(t swarm.Task) string { return "task:" + t.ID },
		filterEnv:    filter.TaskEnv,
		sortKeys: map[string]func(swarm.Task) string{
			"state":   func(t swarm.Task) string { return taskStateSortKey(t.Status.State) },
			"service": func(t swarm.Task) string { return t.ServiceID },
			"node":    func(t swarm.Task) string { return t.NodeID },
		},
	})
	if !ok {
		return
	}

	paged := applyPagination(r.Context(), tasks, p)
	enriched := h.enrichTasks(paged.Items)
	writeLinkTemplate(w, r, "/tasks/{id}")
	writeCollectionResponse(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(enriched, "Task", func(t EnrichedTask) string { return "/tasks/" + t.ID }),
			paged.Total,
			paged.Limit,
			paged.Offset,
		),
		p,
	)
}

func (h *Handlers) HandleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, ok := lookupACL(h, w, r, "task", id, h.cache.GetTask, func(t swarm.Task) string {
		return "task:" + t.ID
	})
	if !ok {
		return
	}
	et := h.enrichTask(task)
	h.setAllow(w, r, "task", id)
	writeCachedJSONTimed(w, r, NewDetailResponse(r.Context(), "/tasks/"+id, "Task", TaskResponse{
		Task:    et,
		Service: TaskServiceRef{AtID: "/services/" + et.ServiceID, Name: et.ServiceName},
		Node:    TaskNodeRef{AtID: "/nodes/" + et.NodeID, Hostname: et.NodeHostname},
	}), task.UpdatedAt)
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
