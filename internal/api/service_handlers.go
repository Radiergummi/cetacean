package api

import (
	"context"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/internal/integrations"
)

// --- Services ---

// lookupServiceACL resolves a service by path ID, checks read ACL, and returns
// it. Returns false (and writes the error response) if not found or denied.
func (h *Handlers) lookupServiceACL(
	w http.ResponseWriter,
	r *http.Request,
) (swarm.Service, bool) {
	return lookupACL(
		h,
		w,
		r,
		"service",
		r.PathValue("id"),
		h.cache.GetService,
		func(s swarm.Service) string {
			return "service:" + s.Spec.Name
		},
	)
}

type ServiceListItem struct {
	swarm.Service
	RunningTasks int `json:"RunningTasks"`
}

func (h *Handlers) HandleListServices(w http.ResponseWriter, r *http.Request) {
	services, p, ok := prepareList(h, w, r, listSpec[swarm.Service]{
		resourceType: "service",
		linkTemplate: "/services/{id}",
		list:         h.cache.ListServices,
		aclResource:  func(s swarm.Service) string { return "service:" + s.Spec.Name },
		searchName:   func(s swarm.Service) string { return s.Spec.Name },
		filterEnv:    filter.ServiceEnv,
		sortKeys: map[string]func(swarm.Service) string{
			"name": func(s swarm.Service) string { return s.Spec.Name },
			"mode": func(s swarm.Service) string {
				if s.Spec.Mode.Global != nil {
					return "Global"
				}
				return "Replicated"
			},
		},
	})
	if !ok {
		return
	}

	paged := applyPagination(r.Context(), services, p)

	items := make([]ServiceListItem, len(paged.Items))
	for i, svc := range paged.Items {
		items[i] = ServiceListItem{
			Service:      svc,
			RunningTasks: h.cache.RunningTaskCount(svc.ID),
		}
	}

	writeLinkTemplate(w, r, "/services/{id}")
	writeCollectionResponse(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(items, "Service", func(s ServiceListItem) string { return "/services/" + s.ID }),
			paged.Total,
			paged.Limit,
			paged.Offset,
		),
		p,
	)
}

func (h *Handlers) HandleGetService(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	detail := ServiceResponse{Service: svc}
	if changes := DiffServiceSpecs(svc.PreviousSpec, &svc.Spec); len(changes) > 0 {
		detail.Changes = changes
	}
	if detected := integrations.Detect(svc.Spec.Labels); len(detected) > 0 {
		detail.Integrations = detected
	}
	h.setAllow(w, r, "service", svc.Spec.Name)
	writeCachedJSONTimed(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id, "Service", detail),
		svc.UpdatedAt,
	)
}

func (h *Handlers) HandleServiceTasks(w http.ResponseWriter, r *http.Request) {
	_, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	tasks := h.enrichTasks(h.cache.ListTasksByService(r.PathValue("id")))
	writeCachedJSON(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(tasks, "Task", func(t EnrichedTask) string { return "/tasks/" + t.ID }),
			len(tasks),
			len(tasks),
			0,
		),
	)
}

func (h *Handlers) HandleServiceLogs(w http.ResponseWriter, r *http.Request) {
	_, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	h.serveLogs(
		w,
		r,
		func(ctx context.Context, tail string, follow bool, since, until string) (LogStream, error) {
			return h.dockerClient.Logs(ctx, docker.ServiceLog, id, tail, follow, since, until)
		},
	)
}
