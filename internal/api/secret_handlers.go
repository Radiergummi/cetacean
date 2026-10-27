package api

import (
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/internal/versions"
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

	writeCachedJSON(w, r, rep)
}

// SecretListItem is a secret as GET /secrets lists it: grouped with its other
// versions, and counting the services that use it, both over what the caller
// may read.
type SecretListItem struct {
	swarm.Secret
	VersionOf       string `json:"VersionOf,omitempty"`
	VersionInferred bool   `json:"VersionInferred,omitempty"`
	UsedBy          int    `json:"UsedBy"`
}

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	render := func(page []swarm.Secret) []SecretListItem {
		grouped := versions.Secrets(readable(h, r, "secret", h.cache.ListSecrets(), secretName))
		items := make([]SecretListItem, len(page))
		for i, sec := range page {
			m := grouped[sec.ID]
			items[i] = SecretListItem{
				Secret:          sec,
				VersionOf:       m.Of,
				VersionInferred: m.Inferred,
				UsedBy:          len(h.filterServiceRefs(r, h.cache.ServicesUsingSecret(sec.ID))),
			}
		}

		return items
	}

	idFunc := func(s SecretListItem) string { return "/secrets/" + s.ID }
	handleListOf(h, w, r, listSpec[swarm.Secret]{
		resourceType: "secret",
		linkTemplate: "/secrets/{id}",
		list:         h.cache.ListSecrets,
		aclName:      secretName,
		searchName:   secretName,
		filterEnv:    filter.Secrets,
		prepare:      cluster.RedactSecrets,
		sortKeys: map[string]func(swarm.Secret) string{
			"name":    func(s swarm.Secret) string { return s.Spec.Name },
			"created": func(s swarm.Secret) string { return s.CreatedAt.String() },
			"updated": func(s swarm.Secret) string { return s.UpdatedAt.String() },
		},
		itemType: "Secret",
		idFunc:   func(s swarm.Secret) string { return "/secrets/" + s.ID },
		rows:     cluster.RowsForSecrets,
	}, idFunc, render)
}

func secretName(s swarm.Secret) string { return s.Spec.Name }
