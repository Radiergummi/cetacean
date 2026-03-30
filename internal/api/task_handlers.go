package api

import (
	"context"
	"fmt"
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
	tasks := h.cache.ListTasks()
	var ok bool
	if tasks, ok = exprFilter(tasks, r.URL.Query().Get("filter"), filter.TaskEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	tasks = sortItems(tasks, p.Sort, p.Dir, map[string]func(swarm.Task) string{
		"state":   func(t swarm.Task) string { return taskStateSortKey(t.Status.State) },
		"service": func(t swarm.Task) string { return t.ServiceID },
		"node":    func(t swarm.Task) string { return t.NodeID },
	})
	paged := applyPagination(tasks, p)
	writePaginationLinks(w, r, paged.Total, paged.Limit, paged.Offset)
	writeJSONWithETag(
		w,
		r,
		NewCollectionResponse(h.enrichTasks(paged.Items), paged.Total, paged.Limit, paged.Offset),
	)
}

func (h *Handlers) HandleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, ok := h.cache.GetTask(id)
	if !ok {
		writeErrorCode(w, r, "TSK002", fmt.Sprintf("task %q not found", id))
		return
	}
	et := h.enrichTask(task)
	writeJSONWithETag(w, r, NewDetailResponse("/tasks/"+id, "Task", map[string]any{
		"task":    et,
		"service": map[string]any{"@id": "/services/" + et.ServiceID, "name": et.ServiceName},
		"node":    map[string]any{"@id": "/nodes/" + et.NodeID, "hostname": et.NodeHostname},
	}))
}

func (h *Handlers) HandleTaskLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetTask(id)
	if !ok {
		writeErrorCode(w, r, "TSK002", fmt.Sprintf("task %q not found", id))
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
