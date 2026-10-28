package api

import (
	"context"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/filter"
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
	DesiredTasks int `json:"DesiredTasks"`

	// A job's current run alone; absent for any other mode.
	CompletedTasks *int `json:"CompletedTasks,omitempty"`
	FailedTasks    *int `json:"FailedTasks,omitempty"`

	ImageUpdate *cluster.ImageUpdate `json:"ImageUpdate,omitempty"`
}

func (h *Handlers) HandleListServices(w http.ResponseWriter, r *http.Request) {
	spec := listSpec[swarm.Service]{
		resourceType: "service",
		linkTemplate: "/services/{id}",
		list:         h.cache.ListServices,
		aclName:      func(s swarm.Service) string { return s.Spec.Name },
		searchName:   func(s swarm.Service) string { return s.Spec.Name },
		filterEnv:    filter.Services,
		sortKeys: map[string]func(swarm.Service) string{
			"name": func(s swarm.Service) string { return s.Spec.Name },
			"mode": filter.ServiceMode,
		},
	}

	// The task and node counts come from the same cache, so the derived validator
	// covers them; see handleList.
	validator := h.derivedETag(r)
	if listNotModified(h, w, r, spec, validator) {
		return
	}

	r = ifRange(r, validator)

	services, _, p, ok := prepareList(h, w, r, spec, cluster.VersionSelection{})
	if !ok {
		return
	}

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		// Counted once: asking per row would walk the task table per service.
		running := h.cache.RunningTaskCounts()
		completed := h.cache.CompletedJobTaskCounts()
		flapping := h.cache.FlappingServices()

		writeListCSV(w, r, "service", services, p, func(page []swarm.Service) []cluster.Row {
			return cluster.RowsForServices(
				page,
				running,
				completed,
				h.cache.EligibleNodeCounts(page...),
				flapping,
			)
		})

		return
	}

	writeLinkTemplate(w, r, spec.linkTemplate)
	writeListPage(
		w,
		r,
		services,
		p,
		validator,
		"Service",
		func(s ServiceListItem) string { return "/services/" + s.ID },
		func(page []swarm.Service) []ServiceListItem {
			tallies := h.cache.TaskTallies()
			eligible := h.cache.EligibleNodeCounts(page...)

			items := make([]ServiceListItem, len(page))
			for i, svc := range page {
				tally := tallies[svc.ID]
				items[i] = ServiceListItem{
					Service:      svc,
					RunningTasks: tally.Running,
					DesiredTasks: cluster.DesiredTaskCount(svc, eligible[svc.ID]),
					ImageUpdate:  h.imageUpdate(svc),
				}

				if cache.IsJobService(svc) {
					items[i].CompletedTasks = new(tally.Completed)
					items[i].FailedTasks = new(tally.Failed)
				}
			}

			return items
		},
	)
}

func (h *Handlers) HandleGetService(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	h.setAllow(w, r, "service", svc.Spec.Name)

	rep, ok := representationOr404(w, r, "service", svc.ID, h.serviceRepresentation)
	if !ok {
		return
	}

	writeCachedJSONTimed(w, r, rep, h.serviceLastModified(svc))
}

// serviceLastModified is the Last-Modified of a service's representation. The
// image check can add an update with no change to the service, and a client
// revalidating on the date alone must not be told it has that.
func (h *Handlers) serviceLastModified(svc swarm.Service) time.Time {
	lastModified := svc.UpdatedAt
	if update := h.imageUpdate(svc); update != nil && update.Since.After(lastModified) {
		lastModified = update.Since
	}

	return lastModified
}

// serviceModified is serviceLastModified for the service a request addresses.
func (h *Handlers) serviceModified(r *http.Request) time.Time {
	svc, _ := h.cache.GetService(r.PathValue("id"))

	return h.serviceLastModified(svc)
}

func (h *Handlers) HandleServiceTasks(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	tasks := cluster.EnrichTasks(
		h.cache,
		h.cache.ListTasksByService(r.PathValue("id")),
		h.readChecker(r),
	)

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		h.writeTaskCSV(w, r, svc.Spec.Name, tasks)
		return
	}

	writeCachedJSON(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(r.Context(), tasks, "Task", enrichedTaskID),
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
