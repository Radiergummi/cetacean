package api

import (
	"fmt"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

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
	writeJSONWithETag(w, r, NewDetailResponse(r.Context(), "/configs/"+id, "Config", map[string]any{
		"config":   cfg,
		"services": h.cache.ServicesUsingConfig(id),
	}))
}

func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	configs := h.cache.ListConfigs()
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
