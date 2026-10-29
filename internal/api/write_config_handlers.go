package api

import (
	"context"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

func (h *Handlers) HandleRemoveConfig(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[swarm.Config]{
		resource: "config",
		getter:   h.cache.GetConfig,
		remove: func(ctx context.Context, key string) error {
			return h.writes.RemoveConfig(ctx, h.caller(r), key)
		},
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
		create: func(ctx context.Context, name string, _ map[string]string, data []byte) (string, error) {
			return h.writes.CreateConfig(ctx, h.caller(r), name, nil, data)
		},
		// Created now and labelled as asked, so it ranks and groups as the
		// real record will.
		buildFallback: func(id, name string, labels map[string]string) any {
			now := time.Now()

			return h.configBody(r, swarm.Config{
				ID:        id,
				CreatedAt: now, UpdatedAt: now,
				Spec: swarm.ConfigSpec{Name: name, Labels: labels},
			})
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
		update:        h.writer.UpdateConfigLabels,
		conflictCode:  "CFG005",
		stackMember:   true,
	})
}
