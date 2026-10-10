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
		aclName:      nodeHostnameOrID,
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

// HandleNodeDrainImpact answers what draining a node would move and what it
// would strand, the assessment MCP's get_topology drain-impact view gives. The
// cluster's tasks, not the node's, since a per-node replica cap is measured
// against what each candidate already runs.
func (h *Handlers) HandleNodeDrainImpact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := lookupACL(h, w, r, "node", id, h.cache.GetNode, nodeResource)
	if !ok {
		return
	}

	identity := auth.IdentityFromContext(r.Context())
	all := h.cache.ListNodes()
	nodes := acl.Filter(h.acl, identity, "read", all, nodeResource)
	services := acl.Filter(
		h.acl, identity, "read",
		h.cache.ListServices(),
		func(s swarm.Service) string { return "service:" + s.Spec.Name },
	)
	tasks := acl.FilterInPlaceNamed(
		h.acl, identity, "read",
		h.cache.ListTasks(),
		"task",
		func(t swarm.Task) string { return t.ID },
	)

	graph := cluster.DrainImpactGraph(node, nodes, tasks, services)
	cluster.NoteHiddenNodes(&graph, len(nodes), len(all)-len(nodes))

	writeCachedJSON(w, r, NewDetailResponse(r.Context(), r.URL.Path, "DrainImpact", graph))
}

func (h *Handlers) HandleNodeTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := lookupACL(h, w, r, "node", id, h.cache.GetNode, nodeResource)
	if !ok {
		return
	}
	// In place and by name, as the list pipeline does: ListTasksByNode hands
	// back a copy nothing else holds, and the matcher wants the name rather
	// than a "task:" string built for each one.
	tasks := acl.FilterInPlaceNamed(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		h.cache.ListTasksByNode(id),
		"task",
		func(t swarm.Task) string { return t.ID },
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
			wrapItems(r.Context(), enriched, "Task", enrichedTaskID),
			len(enriched),
			len(enriched),
			0,
		),
	)
}
