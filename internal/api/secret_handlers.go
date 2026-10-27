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

	writeCachedJSON(w, r, rep)
}

// SecretListItem is a secret as GET /secrets lists it.
type SecretListItem = cluster.VersionedSecret

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	spec := listSpec[swarm.Secret]{
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
		rows:     cluster.RowsForSecrets,
	}

	versions := versionedList(
		h, r, &spec, secretID,
		func(readable []swarm.Secret, canRead func(string) bool) cluster.VersionView {
			return cluster.SecretVersionView(h.cache, readable, canRead)
		},
	)

	handleListOf(h, w, r, spec, func(s SecretListItem) string { return "/secrets/" + s.ID },
		func(page []swarm.Secret) []SecretListItem {
			items := make([]SecretListItem, len(page))
			for i, sec := range page {
				items[i] = SecretListItem{Secret: sec, VersionFields: versions.Fields(sec.ID)}
			}

			return items
		},
	)
}

func secretName(s swarm.Secret) string { return s.Spec.Name }

func secretID(s swarm.Secret) string { return s.ID }
