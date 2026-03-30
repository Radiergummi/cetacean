package api

import (
	"fmt"
	"net/http"

	"github.com/docker/docker/api/types/network"

	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Networks ---

func (h *Handlers) HandleGetNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	net, ok := h.cache.GetNetwork(id)
	if !ok {
		writeErrorCode(w, r, "NET002", fmt.Sprintf("network %q not found", id))
		return
	}
	writeJSONWithETag(w, r, NewDetailResponse("/networks/"+id, "Network", map[string]any{
		"network":  net,
		"services": h.cache.ServicesUsingNetwork(id),
	}))
}

func (h *Handlers) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	networks := h.cache.ListNetworks()
	networks = searchFilter(
		networks,
		r.URL.Query().Get("search"),
		func(n network.Summary) string { return n.Name },
	)
	var ok bool
	if networks, ok = exprFilter(
		networks,
		r.URL.Query().Get("filter"),
		filter.NetworkEnv,
		w,
		r,
	); !ok {
		return
	}
	p := parsePagination(r)
	networks = sortItems(networks, p.Sort, p.Dir, map[string]func(network.Summary) string{
		"name":   func(n network.Summary) string { return n.Name },
		"driver": func(n network.Summary) string { return n.Driver },
		"scope":  func(n network.Summary) string { return n.Scope },
	})
	resp := applyPagination(networks, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeJSONWithETag(w, r, resp)
}
