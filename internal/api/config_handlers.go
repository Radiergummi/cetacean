package api

import (
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Configs ---

func (h *Handlers) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, ok := lookupACL(h, w, r, "config", id, h.cache.GetConfig, func(c swarm.Config) string {
		return "config:" + c.Spec.Name
	})
	if !ok {
		return
	}
	h.setAllow(w, r, "config", cfg.Spec.Name)
	writeCachedJSONTimed(
		w,
		r,
		NewDetailResponse(r.Context(), "/configs/"+id, "Config", ConfigResponse{
			Config:   cfg,
			Services: h.filterServiceRefs(r, h.cache.ServicesUsingConfig(id)),
		}),
		cfg.UpdatedAt,
	)
}

func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	handleList(h, w, r, listSpec[swarm.Config]{
		resourceType: "config",
		linkTemplate: "/configs/{id}",
		list:         h.cache.ListConfigs,
		aclResource:  func(c swarm.Config) string { return "config:" + c.Spec.Name },
		searchName:   func(c swarm.Config) string { return c.Spec.Name },
		filterEnv:    filter.ConfigEnv,
		sortKeys: map[string]func(swarm.Config) string{
			"name":    func(c swarm.Config) string { return c.Spec.Name },
			"created": func(c swarm.Config) string { return c.CreatedAt.String() },
			"updated": func(c swarm.Config) string { return c.UpdatedAt.String() },
		},
		itemType: "Config",
		idFunc:   func(c swarm.Config) string { return "/configs/" + c.ID },
	})
}
