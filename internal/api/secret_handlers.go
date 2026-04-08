package api

import (
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Secrets ---

func (h *Handlers) HandleGetSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sec, ok := lookupACL(h, w, r, "secret", id, h.cache.GetSecret, func(s swarm.Secret) string {
		return "secret:" + s.Spec.Name
	})
	if !ok {
		return
	}
	// Never expose secret data — clear it before responding.
	sec.Spec.Data = nil
	h.setAllow(w, r, "secret", sec.Spec.Name)
	writeCachedJSONTimed(
		w,
		r,
		NewDetailResponse(r.Context(), "/secrets/"+id, "Secret", SecretResponse{
			Secret:   sec,
			Services: h.filterServiceRefs(r, h.cache.ServicesUsingSecret(id)),
		}),
		sec.UpdatedAt,
	)
}

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	handleList(h, w, r, listSpec[swarm.Secret]{
		resourceType: "secret",
		linkTemplate: "/secrets/{id}",
		list:         h.cache.ListSecrets,
		aclResource:  func(s swarm.Secret) string { return "secret:" + s.Spec.Name },
		searchName:   func(s swarm.Secret) string { return s.Spec.Name },
		filterEnv:    filter.SecretEnv,
		prepare: func(secrets []swarm.Secret) []swarm.Secret {
			for i := range secrets {
				secrets[i].Spec.Data = nil
			}
			return secrets
		},
		sortKeys: map[string]func(swarm.Secret) string{
			"name":    func(s swarm.Secret) string { return s.Spec.Name },
			"created": func(s swarm.Secret) string { return s.CreatedAt.String() },
			"updated": func(s swarm.Secret) string { return s.UpdatedAt.String() },
		},
	})
}
