package api

import (
	"fmt"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

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
	// Never expose secret data — clear it before responding.
	sec.Spec.Data = nil
	writeJSONWithETag(w, r, NewDetailResponse(r.Context(), "/secrets/"+id, "Secret", map[string]any{
		"secret":   sec,
		"services": h.cache.ServicesUsingSecret(id),
	}))
}

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	secrets := h.cache.ListSecrets()
	for i := range secrets {
		secrets[i].Spec.Data = nil
	}
	secrets = searchFilter(
		secrets,
		r.URL.Query().Get("search"),
		func(s swarm.Secret) string { return s.Spec.Name },
	)
	var ok bool
	if secrets, ok = exprFilter(secrets, r.URL.Query().Get("filter"), filter.SecretEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	secrets = sortItems(secrets, p.Sort, p.Dir, map[string]func(swarm.Secret) string{
		"name":    func(s swarm.Secret) string { return s.Spec.Name },
		"created": func(s swarm.Secret) string { return s.CreatedAt.String() },
		"updated": func(s swarm.Secret) string { return s.UpdatedAt.String() },
	})
	resp := applyPagination(r.Context(), secrets, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeJSONWithETag(w, r, resp)
}
