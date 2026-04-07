package api

import (
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Configs ---

func (h *Handlers) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, ok := lookupOr404(w, r, "config", id, h.cache.GetConfig)
	if !ok {
		return
	}
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", "config:"+cfg.Spec.Name) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}
	h.setAllow(w, r, "config", cfg.Spec.Name)
	writeCachedJSONTimed(
		w,
		r,
		NewDetailResponse(r.Context(), "/configs/"+id, "Config", ConfigResponse{
			Config: cfg,
			Services: acl.Filter(
				h.acl,
				auth.IdentityFromContext(r.Context()),
				"read",
				h.cache.ServicesUsingConfig(id),
				func(ref cache.ServiceRef) string {
					return "service:" + ref.Name
				},
			),
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
	})
}
