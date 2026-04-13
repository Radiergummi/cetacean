package api

import (
	"net/http"

	"github.com/docker/docker/api/types/network"

	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Networks ---

func (h *Handlers) HandleGetNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	net, ok := lookupACL(
		h,
		w,
		r,
		"network",
		id,
		h.cache.GetNetwork,
		func(n network.Summary) string {
			return "network:" + n.Name
		},
	)
	if !ok {
		return
	}
	h.setAllow(w, r, "network", net.Name)
	writeCachedJSONTimed(
		w,
		r,
		NewDetailResponse(r.Context(), "/networks/"+id, "Network", NetworkResponse{
			Network:  net,
			Services: h.filterServiceRefs(r, h.cache.ServicesUsingNetwork(id)),
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
		itemType: "Network",
		idFunc:   func(n network.Summary) string { return "/networks/" + n.ID },
	})
}
