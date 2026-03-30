package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/internal/integrations"
)

// --- Services ---

type ServiceListItem struct {
	swarm.Service
	RunningTasks int `json:"RunningTasks"`
}

func (h *Handlers) HandleListServices(w http.ResponseWriter, r *http.Request) {
	services := h.cache.ListServices()
	services = searchFilter(
		services,
		r.URL.Query().Get("search"),
		func(s swarm.Service) string { return s.Spec.Name },
	)
	var ok bool
	if services, ok = exprFilter(
		services,
		r.URL.Query().Get("filter"),
		filter.ServiceEnv,
		w,
		r,
	); !ok {
		return
	}
	p := parsePagination(r)
	services = sortItems(services, p.Sort, p.Dir, map[string]func(swarm.Service) string{
		"name": func(s swarm.Service) string { return s.Spec.Name },
		"mode": func(s swarm.Service) string {
			if s.Spec.Mode.Global != nil {
				return "Global"
			}
			return "Replicated"
		},
	})
	paged := applyPagination(r.Context(), services, p)

	items := make([]ServiceListItem, len(paged.Items))
	for i, svc := range paged.Items {
		items[i] = ServiceListItem{
			Service:      svc,
			RunningTasks: h.cache.RunningTaskCount(svc.ID),
		}
	}

	writePaginationLinks(w, r, paged.Total, paged.Limit, paged.Offset)
	writeJSONWithETag(
		w,
		r,
		NewCollectionResponse(r.Context(), items, paged.Total, paged.Limit, paged.Offset),
	)
}

func (h *Handlers) HandleGetService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", fmt.Sprintf("service %q not found", id))
		return
	}
	extra := map[string]any{
		"service": svc,
	}
	if changes := DiffServiceSpecs(svc.PreviousSpec, &svc.Spec); len(changes) > 0 {
		extra["changes"] = changes
	}
	if detected := integrations.Detect(svc.Spec.Labels); len(detected) > 0 {
		extra["integrations"] = detected
	}
	writeJSONWithETag(w, r, NewDetailResponse(r.Context(), "/services/"+id, "Service", extra))
}

func (h *Handlers) HandleServiceTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", fmt.Sprintf("service %q not found", id))
		return
	}
	tasks := h.enrichTasks(h.cache.ListTasksByService(id))
	writeJSONWithETag(w, r, NewCollectionResponse(r.Context(), tasks, len(tasks), len(tasks), 0))
}

func (h *Handlers) HandleServiceLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", fmt.Sprintf("service %q not found", id))
		return
	}
	h.serveLogs(
		w,
		r,
		func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error) {
			return h.dockerClient.Logs(ctx, docker.ServiceLog, id, tail, follow, since, until)
		},
	)
}
