package api

import (
	"context"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func (h *Handlers) HandleRemoveSecret(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[swarm.Secret]{
		resource:     "secret",
		pathKey:      "id",
		getter:       h.cache.GetSecret,
		remove:       h.secretWriter.RemoveSecret,
		conflictCode: "SEC001",
	})
}

func (h *Handlers) HandleCreateSecret(w http.ResponseWriter, r *http.Request) {
	handleCreateDataResource(w, r, createDataResourceSpec{
		resource:     "secret",
		nameErrCode:  "SEC004",
		conflictCode: "SEC003",
		basePath:     "/secrets/",
		typeName:     "Secret",
		create: func(ctx context.Context, name string, data []byte) (string, error) {
			return h.secretWriter.CreateSecret(ctx, swarm.SecretSpec{
				Annotations: swarm.Annotations{Name: name},
				Data:        data,
			})
		},
		buildFallback: func(id string, name string) any {
			return SecretResponse{
				Secret: swarm.Secret{
					ID:   id,
					Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: name}},
				},
				Services: []cache.ServiceRef{},
			}
		},
		buildResponse: func(id string) (any, bool) {
			sec, ok := h.cache.GetSecret(id)
			if !ok {
				return nil, false
			}

			sec.Spec.Data = nil

			return SecretResponse{
				Secret:   sec,
				Services: h.cache.ServicesUsingSecret(id),
			}, true
		},
	})
}

func (h *Handlers) HandleGetSecretLabels(w http.ResponseWriter, r *http.Request) {
	handleGetLabels(w, r, h.acl, getLabelsSpec[swarm.Secret]{
		resource:    "secret",
		pathKey:     "id",
		typeName:    "SecretLabels",
		getter:      h.cache.GetSecret,
		aclResource: func(s swarm.Secret) string { return "secret:" + s.Spec.Name },
		getLabels:   func(s swarm.Secret) map[string]string { return s.Spec.Labels },
	})
}

func (h *Handlers) HandlePatchSecretLabels(w http.ResponseWriter, r *http.Request) {
	handlePatchLabels(w, r, patchLabelsSpec[swarm.Secret]{
		resource:     "secret",
		pathKey:      "id",
		typeName:     "SecretLabels",
		getter:       h.cache.GetSecret,
		getLabels:    func(s swarm.Secret) map[string]string { return s.Spec.Labels },
		update:       h.secretWriter.UpdateSecretLabels,
		conflictCode: "SEC005",
	})
}
