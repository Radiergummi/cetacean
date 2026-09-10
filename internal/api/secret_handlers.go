package api

import (
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
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
	h.setAllow(w, r, "secret", sec.Spec.Name)

	// The representation redacts the secret's data before serializing it.
	rep, ok := representationOr404(w, r, "secret", id, h.secretRepresentation)
	if !ok {
		return
	}

	writeCachedJSONTimed(w, r, rep, sec.UpdatedAt)
}

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	handleList(h, w, r, listSpec[swarm.Secret]{
		resourceType: "secret",
		linkTemplate: "/secrets/{id}",
		list:         h.cache.ListSecrets,
		aclResource:  func(s swarm.Secret) string { return "secret:" + s.Spec.Name },
		searchName:   func(s swarm.Secret) string { return s.Spec.Name },
		filterEnv:    filter.SecretEnv,
		prepare:      cluster.RedactSecrets,
		sortKeys: map[string]func(swarm.Secret) string{
			"name":    func(s swarm.Secret) string { return s.Spec.Name },
			"created": func(s swarm.Secret) string { return s.CreatedAt.String() },
			"updated": func(s swarm.Secret) string { return s.UpdatedAt.String() },
		},
		itemType: "Secret",
		idFunc:   func(s swarm.Secret) string { return "/secrets/" + s.ID },
	})
}
