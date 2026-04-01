package api

import (
	"fmt"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Secrets ---

func (h *Handlers) HandleGetSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sec, ok := h.cache.GetSecret(id)
	if !ok {
		writeErrorCode(w, r, "SEC002", fmt.Sprintf("secret %q not found", id))
		return
	}
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", "secret:"+sec.Spec.Name) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}
	// Never expose secret data — clear it before responding.
	sec.Spec.Data = nil
	h.setAllow(w, r, "secret", sec.Spec.Name)
	writeCachedJSONTimed(
		w,
		r,
		NewDetailResponse(r.Context(), "/secrets/"+id, "Secret", SecretResponse{
			Secret: sec,
			Services: acl.Filter(
				h.acl,
				auth.IdentityFromContext(r.Context()),
				"read",
				h.cache.ServicesUsingSecret(id),
				func(ref cache.ServiceRef) string {
					return "service:" + ref.Name
				},
			),
		}),
		sec.UpdatedAt,
	)
}

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	secrets := h.cache.ListSecrets()
	for i := range secrets {
		secrets[i].Spec.Data = nil
	}
	secrets = acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		secrets,
		func(s swarm.Secret) string {
			return "secret:" + s.Spec.Name
		},
	)
	secrets = searchFilter(
		secrets,
		r.URL.Query().Get("search"),
		func(s swarm.Secret) string { return s.Spec.Name },
	)
	var ok bool
	if secrets, ok = exprFilter(secrets, r.URL.Query().Get("filter"), filter.SecretEnv, w, r); !ok {
		return
	}
	p, err := parsePagination(r)
	if err != nil {
		writeProblem(w, r, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}
	secrets = sortItems(secrets, p.Sort, p.Dir, map[string]func(swarm.Secret) string{
		"name":    func(s swarm.Secret) string { return s.Spec.Name },
		"created": func(s swarm.Secret) string { return s.CreatedAt.String() },
		"updated": func(s swarm.Secret) string { return s.UpdatedAt.String() },
	})
	resp := applyPagination(r.Context(), secrets, p)
	writeCollectionResponse(w, r, resp, p)
}
