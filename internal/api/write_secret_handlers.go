package api

import (
	"context"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

func (h *Handlers) HandleRemoveSecret(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[swarm.Secret]{
		resource: "secret",
		getter:   h.cache.GetSecret,
		remove: func(ctx context.Context, key string) error {
			return h.writes.RemoveSecret(ctx, h.caller(r), key)
		},
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
			return h.writes.CreateSecret(ctx, h.caller(r), name, labels, data)
		},
		// Created now and labelled as asked; see the config's.
		buildFallback: func(id, name string, labels map[string]string) any {
			now := time.Now()

			return h.secretBody(r, swarm.Secret{
				ID:        id,
				CreatedAt: now, UpdatedAt: now,
				Spec: swarm.SecretSpec{Name: name, Labels: labels},
			})
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
		update:        h.writer.UpdateSecretLabels,
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
