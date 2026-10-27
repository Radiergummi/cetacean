package api

import (
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/internal/versions"
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

	rep, ok := representationOr404(w, r, "config", id, h.configRepresentation)
	if !ok {
		return
	}

	writeCachedJSON(w, r, rep)
}

// ConfigListItem is a config as GET /configs lists it; see SecretListItem.
type ConfigListItem struct {
	swarm.Config
	VersionOf       string `json:"VersionOf,omitempty"`
	VersionInferred bool   `json:"VersionInferred,omitempty"`
	UsedBy          int    `json:"UsedBy"`
}

func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	render := func(page []swarm.Config) []ConfigListItem {
		grouped := versions.Configs(readable(h, r, "config", h.cache.ListConfigs(), configName))
		items := make([]ConfigListItem, len(page))
		for i, cfg := range page {
			m := grouped[cfg.ID]
			items[i] = ConfigListItem{
				Config:          cfg,
				VersionOf:       m.Of,
				VersionInferred: m.Inferred,
				UsedBy:          len(h.filterServiceRefs(r, h.cache.ServicesUsingConfig(cfg.ID))),
			}
		}

		return items
	}

	idFunc := func(c ConfigListItem) string { return "/configs/" + c.ID }
	handleListOf(h, w, r, listSpec[swarm.Config]{
		resourceType: "config",
		linkTemplate: "/configs/{id}",
		list:         h.cache.ListConfigs,
		aclName:      configName,
		searchName:   configName,
		filterEnv:    filter.Configs,
		sortKeys: map[string]func(swarm.Config) string{
			"name":    func(c swarm.Config) string { return c.Spec.Name },
			"created": func(c swarm.Config) string { return c.CreatedAt.String() },
			"updated": func(c swarm.Config) string { return c.UpdatedAt.String() },
		},
		itemType: "Config",
		idFunc:   func(c swarm.Config) string { return "/configs/" + c.ID },
		rows:     cluster.RowsForConfigs,
	}, idFunc, render)
}

func configName(c swarm.Config) string { return c.Spec.Name }
