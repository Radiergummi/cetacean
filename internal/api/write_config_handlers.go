package api

import (
	"context"
	"net/http"
	"time"

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
		acl:          h.acl,
		nameErrCode:  "CFG004",
		conflictCode: "CFG003",
		basePath:     "/configs/",
		typeName:     "Config",
		create: func(
			ctx context.Context,
			name string,
			labels map[string]string,
			data []byte,
		) (string, error) {
			id, err := h.configWriter.CreateConfig(ctx, swarm.ConfigSpec{
				Name: name, Labels: labels,
				Data: data,
			})
			if err == nil {
				// Attaching resolves through the cache, which the watcher fills later.
				now := time.Now()
				h.cache.SetConfig(swarm.Config{
					ID:        id,
					CreatedAt: now,
					UpdatedAt: now,
					Spec: swarm.ConfigSpec{
						Annotations: swarm.Annotations{Name: name, Labels: labels},
					},
				})
			}

			return id, err
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

			return h.configBody(r, cfg), true
		},
	})
}

func (h *Handlers) configLabelsSpec() getLabelsSpec[swarm.Config] {
	return getLabelsSpec[swarm.Config]{
		resource:    "config",
		pathKey:     "id",
		typeName:    "ConfigLabels",
		getter:      h.cache.GetConfig,
		aclResource: func(c swarm.Config) string { return "config:" + c.Spec.Name },
		getLabels:   func(c swarm.Config) map[string]string { return c.Spec.Labels },
	}
}

func (h *Handlers) HandleGetConfigLabels(w http.ResponseWriter, r *http.Request) {
	handleGetLabels(w, r, h, h.configLabelsSpec())
}

func (h *Handlers) HandlePatchConfigLabels(w http.ResponseWriter, r *http.Request) {
	handlePatchLabels(w, r, h.acl, patchLabelsSpec[swarm.Config]{
		getLabelsSpec: h.configLabelsSpec(),
		update:        h.configWriter.UpdateConfigLabels,
		conflictCode:  "CFG005",
		stackMember:   true,
	})
}
