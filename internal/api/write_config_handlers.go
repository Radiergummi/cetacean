package api

import (
	"context"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func (h *Handlers) HandleRemoveConfig(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[swarm.Config]{
		resource:     "config",
		pathKey:      "id",
		getter:       h.cache.GetConfig,
		remove:       h.configWriter.RemoveConfig,
		conflictCode: "CFG001",
	})
}

func (h *Handlers) HandleCreateConfig(w http.ResponseWriter, r *http.Request) {
	handleCreateDataResource(w, r, createDataResourceSpec{
		resource:     "config",
		nameErrCode:  "CFG004",
		conflictCode: "CFG003",
		basePath:     "/configs/",
		typeName:     "Config",
		create: func(ctx context.Context, name string, data []byte) (string, error) {
			return h.configWriter.CreateConfig(ctx, swarm.ConfigSpec{
				Annotations: swarm.Annotations{Name: name},
				Data:        data,
			})
		},
		buildFallback: func(id string, name string) any {
			return ConfigResponse{
				Config: swarm.Config{
					ID:   id,
					Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name}},
				},
				Services: []cache.ServiceRef{},
			}
		},
		buildResponse: func(id string) (any, bool) {
			cfg, ok := h.cache.GetConfig(id)
			if !ok {
				return nil, false
			}

			return ConfigResponse{
				Config:   cfg,
				Services: h.cache.ServicesUsingConfig(id),
			}, true
		},
	})
}

func (h *Handlers) HandleGetConfigLabels(w http.ResponseWriter, r *http.Request) {
	handleGetLabels(w, r, h.acl, getLabelsSpec[swarm.Config]{
		resource:    "config",
		pathKey:     "id",
		typeName:    "ConfigLabels",
		getter:      h.cache.GetConfig,
		aclResource: func(c swarm.Config) string { return "config:" + c.Spec.Name },
		getLabels:   func(c swarm.Config) map[string]string { return c.Spec.Labels },
	})
}

func (h *Handlers) HandlePatchConfigLabels(w http.ResponseWriter, r *http.Request) {
	handlePatchLabels(w, r, patchLabelsSpec[swarm.Config]{
		resource:     "config",
		pathKey:      "id",
		getter:       h.cache.GetConfig,
		getLabels:    func(c swarm.Config) map[string]string { return c.Spec.Labels },
		update:       h.configWriter.UpdateConfigLabels,
		conflictCode: "CFG005",
	})
}
