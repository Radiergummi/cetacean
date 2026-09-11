package api

import (
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Nodes ---

func (h *Handlers) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	handleList(h, w, r, listSpec[swarm.Node]{
		resourceType: "node",
		linkTemplate: "/nodes/{id}",
		list:         h.cache.ListNodes,
		aclResource:  nodeResource,
		searchName:   func(n swarm.Node) string { return n.Description.Hostname },
		filterEnv:    filter.NodeEnv,
		sortKeys: map[string]func(swarm.Node) string{
			"hostname":     func(n swarm.Node) string { return n.Description.Hostname },
			"role":         func(n swarm.Node) string { return string(n.Spec.Role) },
			"status":       func(n swarm.Node) string { return string(n.Status.State) },
			"availability": func(n swarm.Node) string { return string(n.Spec.Availability) },
		},
		itemType: "Node",
		idFunc:   func(n swarm.Node) string { return "/nodes/" + n.ID },
		rows:     cluster.RowsForNodes,
	})
}

func (h *Handlers) HandleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := lookupACL(h, w, r, "node", id, h.cache.GetNode, nodeResource)
	if !ok {
		return
	}
	h.setAllow(w, r, "node", node.Description.Hostname)

	rep, ok := representationOr404(w, r, "node", id, h.nodeRepresentation)
	if !ok {
		return
	}

	writeCachedJSONTimed(w, r, rep, node.UpdatedAt)
}

func (h *Handlers) HandleNodeTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := lookupACL(h, w, r, "node", id, h.cache.GetNode, nodeResource)
	if !ok {
		return
	}
	tasks := h.cache.ListTasksByNode(id)
	tasks = acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		tasks,
		func(t swarm.Task) string { return "task:" + t.ID },
	)
	enriched := cluster.EnrichTasks(h.cache, tasks)

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		h.writeTaskCSV(w, r, node.Description.Hostname, enriched)
		return
	}

	writeCachedJSON(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(enriched, "Task", enrichedTaskID),
			len(enriched),
			len(enriched),
			0,
		),
	)
}
