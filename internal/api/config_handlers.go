package api

import (
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
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

	rep, ok := representationOr404(w, r, "config", id, h.configRepresentation)
	if !ok {
		return
	}

	writeCachedJSON(w, r, rep)
}

// ConfigListItem is a config as GET /configs lists it; see SecretListItem.
type ConfigListItem = cluster.VersionedConfig

func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	spec := listSpec[swarm.Config]{
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
		rows:     cluster.RowsForConfigs,
	}

	versions := versionedList(
		h, r, &spec, configID,
		func(readable []swarm.Config, canRead func(string) bool) cluster.VersionView {
			return cluster.ConfigVersionView(h.cache, readable, canRead)
		},
	)

	handleListOf(h, w, r, spec, func(c ConfigListItem) string { return "/configs/" + c.ID },
		func(page []swarm.Config) []ConfigListItem {
			items := make([]ConfigListItem, len(page))
			for i, cfg := range page {
				items[i] = ConfigListItem{Config: cfg, VersionFields: versions.Fields(cfg.ID)}
			}

			return items
		},
	)
}

func configName(c swarm.Config) string { return c.Spec.Name }

func configID(c swarm.Config) string { return c.ID }
