package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
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
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)

	if !ok {
		writeErrorCode(w, r, "SVC003", fmt.Sprintf("service %q not found", id))
		return swarm.Service{}, false
	}

	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", "service:"+svc.Spec.Name) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return swarm.Service{}, false
	}

	return svc, true
}

type ServiceListItem struct {
	swarm.Service
	RunningTasks int `json:"RunningTasks"`
}

func (h *Handlers) HandleListServices(w http.ResponseWriter, r *http.Request) {
	h.setAllowList(w, r, "service")
	services := h.cache.ListServices()
	services = acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		services,
		func(s swarm.Service) string {
			return "service:" + s.Spec.Name
		},
	)
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
	p, err := parsePagination(r)
	if err != nil {
		writeProblem(w, r, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}
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

	writeCollectionResponse(
		w,
		r,
		NewCollectionResponse(r.Context(), items, paged.Total, paged.Limit, paged.Offset),
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
	writeCachedJSON(w, r, NewCollectionResponse(r.Context(), tasks, len(tasks), len(tasks), 0))
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
