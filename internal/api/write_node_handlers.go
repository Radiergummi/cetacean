package api

import (
	"log/slog"
	"net/http"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/auth"
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
		writeErrorCode(
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

	slog.Info("updating node availability", "node", id, "availability", req.Availability)

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
		writeErrorCode(w, r, "NOD005", "role must be one of: worker, manager")
		return
	}

	if _, ok := lookupOr404(w, r, "node", id, h.cache.GetNode); !ok {
		return
	}

	slog.Info("updating node role", "node", id, "role", req.Role)

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

	slog.Info("removing node", "node", id, "force", force)

	err := h.nodeWriter.RemoveNode(r.Context(), id, force)
	if err != nil {
		if !force && (cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err)) {
			writeErrorCode(w, r, "NOD001", err.Error())
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
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}

	managerCount := 0
	for _, n := range h.cache.ListNodes() {
		if n.Spec.Role == swarm.NodeRoleManager {
			managerCount++
		}
	}

	writeCachedJSON(w, r, NewDetailResponse(r.Context(), r.URL.Path, "NodeRole", NodeRoleResponse{
		Role:         string(node.Spec.Role),
		IsLeader:     node.ManagerStatus != nil && node.ManagerStatus.Leader,
		ManagerCount: managerCount,
	}))
}

func (h *Handlers) HandleGetNodeLabels(w http.ResponseWriter, r *http.Request) {
	handleGetLabels(w, r, h.acl, getLabelsSpec[swarm.Node]{
		resource:    "node",
		pathKey:     "id",
		typeName:    "NodeLabels",
		getter:      h.cache.GetNode,
		aclResource: nodeResource,
		getLabels:   func(n swarm.Node) map[string]string { return n.Spec.Labels },
	})
}

func (h *Handlers) HandlePatchNodeLabels(w http.ResponseWriter, r *http.Request) {
	handlePatchLabels(w, r, patchLabelsSpec[swarm.Node]{
		resource:     "node",
		pathKey:      "id",
		typeName:     "NodeLabels",
		getter:       h.cache.GetNode,
		getLabels:    func(n swarm.Node) map[string]string { return n.Spec.Labels },
		update:       h.nodeWriter.UpdateNodeLabels,
		conflictCode: "NOD002",
	})
}
