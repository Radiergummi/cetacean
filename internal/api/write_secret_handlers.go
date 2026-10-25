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
		acl:          h.acl,
		entries:      h.secretEntries,
		nameErrCode:  "SEC004",
		conflictCode: "SEC003",
		basePath:     "/secrets/",
		typeName:     "Secret",
		create: func(
			ctx context.Context,
			name string,
			labels map[string]string,
			data []byte,
		) (string, error) {
			id, err := h.secretWriter.CreateSecret(ctx, swarm.SecretSpec{
				Name: name, Labels: labels,
				Data: data,
			})
			if err == nil {
				// Attaching resolves through the cache, which the watcher fills later.
				h.cache.SetSecret(swarm.Secret{
					ID: id,
					Spec: swarm.SecretSpec{
						Annotations: swarm.Annotations{Name: name, Labels: labels},
					},
				})
			}

			return id, err
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

			return h.secretBody(r, sec), true
		},
	})
}

func (h *Handlers) secretLabelsSpec() getLabelsSpec[swarm.Secret] {
	return getLabelsSpec[swarm.Secret]{
		resource:    "secret",
		pathKey:     "id",
		typeName:    "SecretLabels",
		getter:      h.cache.GetSecret,
		aclResource: func(s swarm.Secret) string { return "secret:" + s.Spec.Name },
		getLabels:   func(s swarm.Secret) map[string]string { return s.Spec.Labels },
	}
}

func (h *Handlers) HandleGetSecretLabels(w http.ResponseWriter, r *http.Request) {
	handleGetLabels(w, r, h, h.secretLabelsSpec())
}

func (h *Handlers) HandlePatchSecretLabels(w http.ResponseWriter, r *http.Request) {
	handlePatchLabels(w, r, h.acl, patchLabelsSpec[swarm.Secret]{
		getLabelsSpec: h.secretLabelsSpec(),
		update:        h.secretWriter.UpdateSecretLabels,
		conflictCode:  "SEC005",
		stackMember:   true,
	})
}

// HandleGetSecretEntry serves what an MCP caller chose for a secret it asked
// the person to enter: name, labels and encoding, never a value. Only the
// person whose MCP client opened it may read it.
func (h *Handlers) HandleGetSecretEntry(w http.ResponseWriter, r *http.Request) {
	entry, ok := lookupEntry(w, r, h.secretEntries, r.PathValue("id"))
	if !ok {
		return
	}

	writeAllow(w, readOnly)
	writeCachedJSON(w, r, NewDetailResponse(
		r.Context(), "/secret-entries/"+entry.ID, "SecretEntry", entry,
	))
}
