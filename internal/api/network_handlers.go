package api

import (
	"net/http"

	"github.com/docker/docker/api/types/network"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Networks ---

func (h *Handlers) HandleGetNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	net, ok := lookupOr404(w, r, "network", id, h.cache.GetNetwork)
	if !ok {
		return
	}
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", "network:"+net.Name) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}
	h.setAllow(w, r, "network", net.Name)
	writeCachedJSONTimed(
		w,
		r,
		NewDetailResponse(r.Context(), "/networks/"+id, "Network", NetworkResponse{
			Network: net,
			Services: acl.Filter(
				h.acl,
				auth.IdentityFromContext(r.Context()),
				"read",
				h.cache.ServicesUsingNetwork(id),
				func(ref cache.ServiceRef) string {
					return "service:" + ref.Name
				},
			),
		}),
		net.Created,
	)
}

func (h *Handlers) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	handleList(h, w, r, listSpec[network.Summary]{
		resourceType: "network",
		linkTemplate: "/networks/{id}",
		list:         h.cache.ListNetworks,
		aclResource:  func(n network.Summary) string { return "network:" + n.Name },
		searchName:   func(n network.Summary) string { return n.Name },
		filterEnv:    filter.NetworkEnv,
		sortKeys: map[string]func(network.Summary) string{
			"name":   func(n network.Summary) string { return n.Name },
			"driver": func(n network.Summary) string { return n.Driver },
			"scope":  func(n network.Summary) string { return n.Scope },
		},
	})
}
