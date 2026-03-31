package api

import (
	"fmt"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Configs ---

func (h *Handlers) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		writeErrorCode(w, r, "CFG002", fmt.Sprintf("config %q not found", id))
		return
	}
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", "config:"+cfg.Spec.Name) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}
	h.setAllow(w, r, "config", cfg.Spec.Name)
	writeJSONWithETag(w, r, NewDetailResponse(r.Context(), "/configs/"+id, "Config", ConfigResponse{
		Config:   cfg,
		Services: h.cache.ServicesUsingConfig(id),
	}))
}

func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	configs := h.cache.ListConfigs()
	configs = acl.Filter(h.acl, auth.IdentityFromContext(r.Context()), "read", configs, func(c swarm.Config) string {
		return "config:" + c.Spec.Name
	})
	configs = searchFilter(
		configs,
		r.URL.Query().Get("search"),
		func(c swarm.Config) string { return c.Spec.Name },
	)
	var ok bool
	if configs, ok = exprFilter(configs, r.URL.Query().Get("filter"), filter.ConfigEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	configs = sortItems(configs, p.Sort, p.Dir, map[string]func(swarm.Config) string{
		"name":    func(c swarm.Config) string { return c.Spec.Name },
		"created": func(c swarm.Config) string { return c.CreatedAt.String() },
		"updated": func(c swarm.Config) string { return c.UpdatedAt.String() },
	})
	resp := applyPagination(r.Context(), configs, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeJSONWithETag(w, r, resp)
}
