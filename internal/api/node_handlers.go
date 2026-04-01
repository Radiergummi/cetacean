package api

import (
	"fmt"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Nodes ---

func (h *Handlers) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes := h.cache.ListNodes()
	nodes = acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		nodes,
		func(n swarm.Node) string {
			return "node:" + n.Description.Hostname
		},
	)
	nodes = searchFilter(
		nodes,
		r.URL.Query().Get("search"),
		func(n swarm.Node) string { return n.Description.Hostname },
	)
	var ok bool
	if nodes, ok = exprFilter(nodes, r.URL.Query().Get("filter"), filter.NodeEnv, w, r); !ok {
		return
	}
	p := parsePagination(r)
	nodes = sortItems(nodes, p.Sort, p.Dir, map[string]func(swarm.Node) string{
		"hostname":     func(n swarm.Node) string { return n.Description.Hostname },
		"role":         func(n swarm.Node) string { return string(n.Spec.Role) },
		"status":       func(n swarm.Node) string { return string(n.Status.State) },
		"availability": func(n swarm.Node) string { return string(n.Spec.Availability) },
	})
	resp := applyPagination(r.Context(), nodes, p)
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeCachedJSON(w, r, resp)
}

func (h *Handlers) HandleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", fmt.Sprintf("node %q not found", id))
		return
	}
	if !h.acl.Can(
		auth.IdentityFromContext(r.Context()),
		"read",
		"node:"+node.Description.Hostname,
	) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}
	h.setAllow(w, r, "node", node.Description.Hostname)
	writeCachedJSONTimed(w, r, NewDetailResponse(r.Context(), "/nodes/"+id, "Node", NodeResponse{
		Node: node,
	}), node.UpdatedAt)
}

func (h *Handlers) HandleNodeTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", fmt.Sprintf("node %q not found", id))
		return
	}
	tasks := h.enrichTasks(h.cache.ListTasksByNode(id))
	writeCachedJSON(w, r, NewCollectionResponse(r.Context(), tasks, len(tasks), len(tasks), 0))
}
