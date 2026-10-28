package api

import (
	"log/slog"
	"net/http"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
)

type updateAvailabilityRequest struct {
	Availability string `json:"availability"`
}

func (h *Handlers) HandleUpdateNodeAvailability(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, ok := decodeJSON[updateAvailabilityRequest](w, r)
	if !ok {
		return
	}

	var availability swarm.NodeAvailability
	switch req.Availability {
	case "active":
		availability = swarm.NodeAvailabilityActive
	case "drain":
		availability = swarm.NodeAvailabilityDrain
	case "pause":
		availability = swarm.NodeAvailabilityPause
	default:
		problem.Write(
			w,
			r,
			"NOD004",
			"availability must be one of: active, drain, pause",
		)
		return
	}

	if _, ok := lookupOr404(w, r, "node", id, h.cache.GetNode); !ok {
		return
	}

	slog.InfoContext(
		r.Context(),
		"updating node availability",
		"node",
		id,
		"availability",
		req.Availability,
	)

	writeNodeMutation(w, r, id, func() (swarm.Node, error) {
		return h.nodeWriter.UpdateNodeAvailability(r.Context(), id, availability)
	})
}

type updateRoleRequest struct {
	Role string `json:"role"`
}

func (h *Handlers) HandleUpdateNodeRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, ok := decodeJSON[updateRoleRequest](w, r)
	if !ok {
		return
	}

	var role swarm.NodeRole
	switch req.Role {
	case "worker":
		role = swarm.NodeRoleWorker
	case "manager":
		role = swarm.NodeRoleManager
	default:
		problem.Write(w, r, "NOD005", "role must be one of: worker, manager")
		return
	}

	if _, ok := lookupOr404(w, r, "node", id, h.cache.GetNode); !ok {
		return
	}

	slog.InfoContext(r.Context(), "updating node role", "node", id, "role", req.Role)

	writeNodeMutation(w, r, id, func() (swarm.Node, error) {
		return h.nodeWriter.UpdateNodeRole(r.Context(), id, role)
	})
}

func (h *Handlers) HandleRemoveNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if _, ok := lookupOr404(w, r, "node", id, h.cache.GetNode); !ok {
		return
	}

	force := r.URL.Query().Get("force") == "true"

	slog.InfoContext(r.Context(), "removing node", "node", id, "force", force)

	err := h.nodeWriter.RemoveNode(r.Context(), id, force)
	if err != nil {
		if cluster.IsRemovalConflict(err, "node") {
			writeRemovalConflict(w, r, err, "NOD001", "node", id)
			return
		}
		writeDockerError(w, r, err, "node", id)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleGetNodeRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	node, ok := lookupOr404(w, r, "node", id, h.cache.GetNode)
	if !ok {
		return
	}
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", nodeResource(node)) {
		problem.Write(w, r, "ACL001", "access denied")
		return
	}

	rep, ok := representationOr404(w, r, "node", id, h.nodeRoleRepresentation)
	if !ok {
		return
	}
	h.setAllowSubResource(w, r, nodeResource(node))

	writeCachedJSON(w, r, rep)
}

func (h *Handlers) nodeLabelsSpec() getLabelsSpec[swarm.Node] {
	return getLabelsSpec[swarm.Node]{
		resource:    "node",
		typeName:    "NodeLabels",
		getter:      h.cache.GetNode,
		aclResource: nodeResource,
		getLabels:   func(n swarm.Node) map[string]string { return n.Spec.Labels },
	}
}

func (h *Handlers) HandleGetNodeLabels(w http.ResponseWriter, r *http.Request) {
	handleGetLabels(w, r, h, h.nodeLabelsSpec())
}

func (h *Handlers) HandlePatchNodeLabels(w http.ResponseWriter, r *http.Request) {
	handlePatchLabels(w, r, h.acl, patchLabelsSpec[swarm.Node]{
		getLabelsSpec: h.nodeLabelsSpec(),
		update:        h.nodeWriter.UpdateNodeLabels,
		conflictCode:  "NOD002",
	})
}
